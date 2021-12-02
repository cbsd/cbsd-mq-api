package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	ServerUrl       string `json:"server_url"`
	CbsdEnv         string `json:"cbsdenv"`
	Broker          string `json:"broker"`
	ImageList       string `json:"imagelist"`
	Recomendation   string `json:"recomendation"`
	Freejname       string `json:"freejname"`
	Cloud_images_list string `json:"cloud_images_list"`
	Iso_images_list   string `json:"iso_images_list"`
	BeanstalkConfig `json:"beanstalkd"`
}

func LoadConfiguration(file string) (Config, error) {
	var config Config
	configFile, err := os.Open(file)
	defer configFile.Close()

	if err != nil {
		fmt.Println(err.Error())
		return config, err
	}

	jsonParser := json.NewDecoder(configFile)
	err = jsonParser.Decode(&config)

	if err != nil {
		fmt.Printf("config error: %s: %s\n", file, err.Error())
		return config, err
	}

	fmt.Printf("Using config file: %s\n", file)
	return config, err
}
