package main

import "os"

func apiAddress() string {
	if address := os.Getenv("API_ADDR"); address != "" {
		return address
	}

	return "127.0.0.1:8443"
}
