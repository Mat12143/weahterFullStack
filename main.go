package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/bitly/go-simplejson"
	"github.com/gorilla/mux"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Structs
type Measurement struct {
	Id int 					`gorm:"primary_key" json:"id"`
	Hour int
	Date string	
	Temperature float32
	WindSpeed float32 
}

// The Weather API response struct
type WeatherResponse struct {
	CurrentWeather struct {
		Temperature float32 `json:"temperature"`
		WindSpeed float32 `json:"windspeed"`
	} `json:"current_weather"`
}

// Latitude e Longitude of the city you wanna track the temperature
const LATITUDE = 48.135125
const LONGITUDE = 11.581981

// Api URL
var weatherApiUrl = fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&current_weather=true&timezone=auto", LATITUDE, LONGITUDE)

// Discord webhook for errors alerting
const DISCORD_WEBHOOK = "https://discord.com/api/webhooks/1137780009650626711/5XiJinHi7UD_rVmzP02pNBcCfhKn5rXaqoTqZjILkwbbPrJW5N0_GwnM41zleXBciePZ"

var db, err = gorm.Open(sqlite.Open("database.db"), &gorm.Config{})

const DATE_FORMAT = time.DateOnly

var HOUR_TO_RECORD []int

func main() {

	// The hours when you wanna track the temperature
	HOUR_TO_RECORD = []int{10, 15, 20}

	db.AutoMigrate(&Measurement{})
	
	if err != nil {
		logProblemToDS(err)
		log.Panic(err)
	}
	// Main thread for the API and the other thread for data gathering
	go timePolling()

	// Frontend API
	r := mux.NewRouter()
	r.HandleFunc("/average", handleAverage)

	log.Println("Web server started...")

	srv := &http.Server{
		Addr: "0.0.0.0:80",
		Handler: r,
	}
	srv.ListenAndServe()
	

}

// ----------------------------------------------------------------------
// Temperature gathering thread
// ----------------------------------------------------------------------

// Check for the time and create a record or wait...
func timePolling() {

	for {
		now := time.Now()
		hour := now.Hour()

		if (checkItemInArray(hour, HOUR_TO_RECORD)) {
			var m Measurement
		
			err := db.Last(&m, "Hour = ? AND Date = ?", hour, now.Format(DATE_FORMAT)).Error
			
			if err != nil {
				createRecord()
			}
	
			time.Sleep(10 * time.Minute)
			// Less time more checks... but less risk of missing the hour time range
		}
	}
}

func createRecord() {

	// Api request
	resp, err := http.Get(weatherApiUrl)

	if err != nil || resp.StatusCode != 200 {
		logProblemToDS(err)
		log.Panic(err)
	}

	bResp, err := io.ReadAll(resp.Body)

	if err != nil {
		logProblemToDS(err)
		log.Panic(err)
	}

	jsonResp := string(bResp)

	var w WeatherResponse

	// From JSON to struct
	err = json.Unmarshal([]byte(jsonResp), &w)

	if err != nil {
		logProblemToDS(err)
		log.Panic(err)
	}

	now := time.Now()

	var m []Measurement

	// To generate an incremental id for the database to properly order the data
	db.Find(&m)

	if err := db.Create(&Measurement{ len(m) + 1, now.Hour(), now.Format(DATE_FORMAT), w.CurrentWeather.Temperature, w.CurrentWeather.WindSpeed}).Error; err != nil {
		logProblemToDS(err)
		log.Panic(err)
	}

	log.Println("Record saved")
}

// ----------------------------------------------------------------------
// Backend to Frontend API
// ----------------------------------------------------------------------

func handleAverage(w http.ResponseWriter, r *http.Request) {


	var m [3][]Measurement

	for i := 0; i < 3; i++ {
		db.Find(&m[i], "Hour = ?", HOUR_TO_RECORD[i])
	}

	var average [3]float64

	for x := range(m) {
		sum := 0.0
		for _, y := range(m[x]) {
			sum += float64(y.Temperature)
		}
		average[x] = sum / float64(len(m[x]))
	}


	json := simplejson.New()
	for x, i := range(average) {
		if math.IsNaN(i) {
			json.Set(fmt.Sprint(HOUR_TO_RECORD[x]), "null")
		} else {

			json.Set(fmt.Sprint(HOUR_TO_RECORD[x]), i)
		}
	}

	payload, err := json.MarshalJSON()

	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	w.Write(payload)
}


// ----------------------------------------------------------------------
// Utils functions
// ----------------------------------------------------------------------

func checkItemInArray(selecItem int, array []int) bool {

	for _, item := range array {
		if item == selecItem {
			return true
		}
	}
	return false

}

func logProblemToDS(err error) {

	body := []byte(
		fmt.Sprintf(`{
		"embeds": [
		  {
			"title": "Error while trying to save a record",
			"description" : "%s",
			"color" : 15548997
		  }
		]
	  }`, err.Error()),
	)

	resp, _ := http.Post(DISCORD_WEBHOOK, "application/json", bytes.NewBuffer(body))

	fmt.Printf("resp.StatusCode: %v\n", resp.StatusCode)
}
