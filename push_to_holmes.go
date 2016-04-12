package main

import (
	"bufio"
	"bytes"
//	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"gopkg.in/mgo.v2/bson"
	"sync"
)

type critsSample struct {
	Id  bson.ObjectId `json:"_id" bson:"_id,omitempty"`
	MD5 string        `json:"md5"`
}

var (
	client          *http.Client
	critsFileServer string
	holmesStorage   string
	wg              sync.WaitGroup
)

func init() {
	// http client
	tr := &http.Transport{}
	/*
		// Disable SSL verification
		tr = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	*/

	client = &http.Client{Transport: tr}
}

func main() {
	fmt.Println("Running...")

	// cmd line flags
	var fPath string
	flag.StringVar(&fPath, "file", "", "List of samples (MD5, SHAX, CRITs ID) to copy from CRITs to Totem")
	flag.StringVar(&critsFileServer, "cfs", "", "Full URL to your CRITs file server")
	flag.StringVar(&holmesStorage, "storage", "", "Full URL to your Holmes-Storage server")
	flag.Parse()

	fmt.Sprintf("Copying samples from %s", fPath)

	file, err := os.Open(fPath)
	if err != nil {
		fmt.Println("Couln't open file containing sample list!", err.Error())
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	// line by line
	for scanner.Scan() {
		wg.Add(1)
		go copySample(scanner.Text())
	}
	wg.Wait()
}

func copySample(hash string) {
	defer wg.Done()
	// set all necessary parameters
	parameters := map[string]string{
		"user_id": "1",
		"source":  "CRITs",
		"name":    hash,
		"date":    time.Now().Format(time.RFC3339),
	}

	request, err := buildRequest(holmesStorage+"/samples/", parameters, hash)
	if err != nil {
		log.Fatal("ERROR: " + err.Error())
	}
	client := &http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		log.Fatal("ERROR: " + err.Error())
		return
	}

	body := &bytes.Buffer{}
	_, err = body.ReadFrom(resp.Body)
	if err != nil {
		log.Fatal("ERROR: " + err.Error())
	}
	resp.Body.Close()

	fmt.Println("Uploaded", hash)
	fmt.Println(resp.StatusCode)
	fmt.Println(body)
	fmt.Println("-------------------------------------------")
}

func buildRequest(uri string, params map[string]string, hash string) (*http.Request, error) {
	var r io.Reader

	// check if local file
	r, err := os.Open(hash)
	if err != nil {
		// not a local file
		// try to get file from crits file server

		cId := &critsSample{}
		if err := bson.Unmarshal([]byte(hash), cId); err != nil {
			return nil, err
		}
		rawId := cId.Id.Hex()

		resp, err := client.Get(critsFileServer + "/" + rawId)
		if err != nil {
			return nil, err
		}
		defer SafeResponseClose(resp)

		// return if file does not exist
		if resp.StatusCode != 200 {
			return nil, errors.New("Couldn't download file")
		}

		r = resp.Body
	}

	// build Holmes-Storage PUT request
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	// TODO: find real name somehow
	part, err := writer.CreateFormFile("sample", hash)
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(part, r)
	if err != nil {
		return nil, err
	}

	for key, val := range params {
		err = writer.WriteField(key, val)
		if err != nil {
			return nil, err
		}
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest("PUT", uri, body)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Content-Type", writer.FormDataContentType())

	return request, nil
}

func SafeResponseClose(r *http.Response) {
	if r == nil {
		return
	}

	io.Copy(ioutil.Discard, r.Body)
	r.Body.Close()
}
