package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
)

// Define a struct for the data you expect to receive from the ESP32
type DeviceUpdate struct {
	IP         string          `json:"ip"`
	Name       string          `json:"name"`
	Buttons    map[string]bool `json:"buttons"`
	Checkboxes map[string]bool `json:"checkboxes"`
}

// Define a struct to hold multiple DeviceUpdate structs
type Devices struct {
	Devices []DeviceUpdate `json:"devices"`
}

func updateHandler(w http.ResponseWriter, r *http.Request) {
	// Read the request body
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Decode the JSON
	var update DeviceUpdate
	err = json.Unmarshal(body, &update)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Read existing data
	file, err := os.OpenFile("payload.json", os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		http.Error(w, "Unable to open data file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	var devices Devices
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&devices); err != nil && err != io.EOF {
		http.Error(w, "Unable to decode data file", http.StatusInternalServerError)
		return
	}

	// Check if IP already exists
	for _, device := range devices.Devices {
		if device.IP == update.IP {
			fmt.Fprintf(w, "Device with IP %s already exists!", update.IP)
			return
		}
	}

	// Append new device
	devices.Devices = append(devices.Devices, update)

	// Move the file pointer back to the beginning of the file
	file.Seek(0, 0)
	file.Truncate(0)
	// Write back to file
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(devices); err != nil {
		http.Error(w, "Unable to encode data to file", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Payload saved!")
}

func renderHandler(w http.ResponseWriter, r *http.Request) {
	// Open the data file
	file, err := os.Open("payload.json")
	if err != nil {
		http.Error(w, "Unable to open data file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Decode the data
	var devices Devices
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&devices); err != nil {
		http.Error(w, "Unable to decode data file", http.StatusInternalServerError)
		return
	}

	// Parse the template file
	tmpl, err := template.ParseFiles("/app/templates/index.html")
	if err != nil {
		http.Error(w, "Unable to load template", http.StatusInternalServerError)
		return
	}

	// Execute the template, passing the devices data
	if err := tmpl.Execute(w, devices); err != nil {
		http.Error(w, "Unable to execute template", http.StatusInternalServerError)
		log.Println(err)
	}
}

func main() {
	http.HandleFunc("/update", updateHandler)
	http.HandleFunc("/", renderHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
