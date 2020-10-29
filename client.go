package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	defaultInterval = time.Second * 5
)

type client struct {
	token     string
	targetURL string
	client    http.Client
	interval  time.Duration
	log       *log.Logger
}

func newClient(targetURL, tokenFile string) (*client, error) {
	_, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	rawToken, err := ioutil.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}
	return &client{
		targetURL: targetURL,
		token:     strings.TrimSpace(string(rawToken)),
		interval:  defaultInterval,
		log:       log.New(os.Stderr, "", log.LstdFlags),
	}, nil
}

func (c *client) run() error {
	c.log.Printf("start client: %s", c.targetURL)
	for {
		err := c.call()
		if err != nil {
			c.log.Println(err)
		}
		time.Sleep(c.interval)
	}
}

func (c *client) call() error {
	req, err := http.NewRequest("GET", c.targetURL, nil)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	c.log.Printf("target=%s, status=%d, response: '%s'", c.targetURL, resp.StatusCode, string(body))
	return nil
}
