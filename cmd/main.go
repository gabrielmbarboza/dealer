package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gabrielmbarboza/dealer/internal/dealer"
	"gopkg.in/yaml.v3"
)

type Service struct {
	Name      string   `yaml:"name"`
	Path      string   `yaml:"path"`
	OriginUrl string   `yaml:"origin_url"`
	Methods   []string `yaml:"methods"`
}

type Config struct {
	Services []Service `yaml:"services"`
}

func main() {
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe("0.0.0.0:3000", nil))
}

func handler(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.RequestURI()

	w.Header().Set("Content-Type", "application/json")

	if strings.Compare(uri, "/") == 0 {
		data, err := json.Marshal(dealer.ProjectInfo())
		if err != nil {
			log.Fatal(err)
		}
		w.Write(data)
		return
	}

	servicePath := fmt.Sprintf("/%s", strings.Split(uri, "/")[1])

	if servicePath == "" {
		log.Fatal("Could not find the service")
	}

	basePath, _ := os.Getwd()

	configData, err := os.ReadFile(fmt.Sprintf("%s/%s", basePath, "config.yml"))
	if err != nil {
		panic(err)
	}

	config := Config{}

	yaml.Unmarshal(configData, &config)

	service := discoverService(servicePath, config.Services)

	fmt.Println(fmt.Sprintf("%s%s", service.OriginUrl, strings.ReplaceAll(r.URL.Path, service.Path, "")))
}

func discoverService(path string, services []Service) Service {
	for _, service := range services {
		if service.Path == path {
			fmt.Println(service)
			return service
		}
	}

	return Service{}
}
