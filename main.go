package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

type oldMsgFormat struct {
	Name    string
	Columns []string
	Points  [][]interface{}
}

func handleError(w http.ResponseWriter, r *http.Request, code int, long string) {
	w.WriteHeader(code)
	io.WriteString(w, "HTTP "+strconv.Itoa(code)+": "+http.StatusText(code))
}

func sendToServer(username, password, database string, msgs []oldMsgFormat) error {
	for _, msg := range msgs {
		for _, point := range msg.Points {
			kvStr, err := mkKeyValueString(msg.Columns, point)
			if err != nil {
				log.Printf("Couldn't format point: %s", err)
				continue
			}

			url := fmt.Sprintf(
				"http://%s/write?db=%s&u=%s&p=%s",
				*server,
				database,
				username,
				password,
			)
			body := msg.Name + " " + kvStr
			if *verbose {
				log.Printf("%s %s", url, body)
			}

			resp, err := http.Post(url, "text/plain", strings.NewReader(body))
			defer resp.Body.Close()
			if err != nil {
				return fmt.Errorf("Failed to send to server: %s", err)
			}
			if resp.StatusCode >= 300 {
				buf := bytes.NewBuffer(nil)
				io.Copy(buf, resp.Body)
				return fmt.Errorf("Got HTTP %d from server: %s", resp.StatusCode, buf)
			}
		}
	}

	return nil
}

func mkKeyValueString(keys []string, values []interface{}) (string, error) {
	if len(keys) != len(values) {
		return "", fmt.Errorf("keys and values are different length")
	}

	var pairs []string
	for i, k := range keys {

		// FIXME: Hardcoded field conversions
		// Some previous uptime columns were float, some int :/
		// Rename as per https://github.com/influxdata/influxdb/issues/2651
		if k == "uptime" {
			pairs = append(pairs, fmt.Sprintf("alive=%di", int64(values[i].(float64))))
			continue
		}
		// Make all of these into integers
		if k == "relay" {
			pairs = append(pairs, fmt.Sprintf("%s=%di", k, int64(values[i].(float64))))
			continue
		}

		switch values[i].(type) {
		// FIXME: Add bool support
		case string:
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, values[i]))
		case float64:
			pairs = append(pairs, fmt.Sprintf("%s=%f", k, values[i]))
		}
	}
	return strings.Join(pairs, ","), nil
}

func handleRequest(w http.ResponseWriter, r *http.Request) {

	decoder := json.NewDecoder(r.Body)
	var msgs []oldMsgFormat
	err := decoder.Decode(&msgs)
	if err != nil {
		log.Printf("HTTP 400: %s", err)
		handleError(w, r, 400, "Could not parse request")
		return
	}

	if err = sendToServer(r.FormValue("u"), r.FormValue("p"), strings.Split(r.URL.Path, "/")[2], msgs); err != nil {
		log.Printf("HTTP 500: %s", err)
		handleError(w, r, 500, "Could not send to server")
		return
	}
}

var bind = flag.String("bind", ":8086", "Interface:Port to listen on")
var server = flag.String("server", "localhost:8886", "Hostname:Port of real server")
var verbose = flag.Bool("verbose", false, "Show every translated request")

func main() {
	flag.Parse()

	serverUrl, err := url.Parse("http://" + *server)
	if err != nil {
		panic(err.Error())
	}

	http.HandleFunc("/db/", handleRequest)
	http.Handle("/", httputil.NewSingleHostReverseProxy(serverUrl))
	log.Fatal(http.ListenAndServe(*bind, nil))
}
