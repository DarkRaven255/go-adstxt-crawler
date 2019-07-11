package main

import (
	"log"

	"github.com/ehulsbosch/go-adstxt-crawler"
)

func main() {

	// fetch and download Ads.txt file from remote host
	req, err := adstxt.NewRequest("http://forum.mototurismo.it")
	if err != nil {
		log.Fatal(err)
	}
	res, err := adstxt.Get(req)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(res.Records)
}
