package main

import (
	"log"
	"net/http"
	"os"

	"github.com/jooservices/xflickr/internal/fakeflickr"
)

func main() {
	if err := run(); err != nil {
		exitWithError(err)
	}
}

var exitWithError = func(err error) { log.Fatal(err) }

var listenAndServe = http.ListenAndServe

func run() error {
	dataset, err := fakeflickr.LoadDataset(os.Getenv("FAKEFLICKR_SEED"))
	if err != nil {
		return err
	}
	address := os.Getenv("FAKEFLICKR_ADDR")
	if address == "" {
		address = ":8088"
	}
	scenarios := fakeflickr.NewScenarios(fakeflickr.ScenarioConfigFromEnvironment())
	log.Printf("fake Flickr server starting")
	return listenAndServe(address, fakeflickr.NewServer(dataset, scenarios))
}
