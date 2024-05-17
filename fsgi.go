package main

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	s := http.Server{
		Addr:    "127.0.0.1:8998",
		Handler: requestHandler{},
	}
	log.Fatal(s.ListenAndServe())
}

type requestHandler struct{}

func (requestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		writeServerError(w, 500, "unable to create temporary directory\n")
		return
	}
	// TODO defer os.RemoveAll(dir)

	// file permission for directories
	var dPerm fs.FileMode = 0750
	// file permission for regular files
	var fPerm fs.FileMode = 0640

	rDir := filepath.Join(dir, "request")

	err = os.MkdirAll(filepath.Join(rDir, "headers"), dPerm)
	if err != nil {
		writeServerError(w, 500, "unable to create request/headers directory\n")
		return
	}

	err = os.Mkdir(filepath.Join(rDir, "query"), dPerm)
	if err != nil {
		writeServerError(w, 500, "unable to create request/query directory\n")
		return
	}

	err = os.WriteFile(filepath.Join(rDir, "method"), []byte(r.Method), fPerm)
	if err != nil {
		writeServerError(w, 500, "unable to create request/method file\n")
		return
	}

	err = os.WriteFile(filepath.Join(rDir, "path"), []byte(r.URL.Path), fPerm)
	if err != nil {
		writeServerError(w, 500, "unable to create request/path file\n")
		return
	}

	err = os.WriteFile(filepath.Join(rDir, "protocol"), []byte(r.Proto), fPerm)
	if err != nil {
		writeServerError(w, 500, "unable to create request/protocol file\n")
		return
	}

	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeClientError(w, 400, "bad query: "+r.URL.RawQuery)
		return
	}
	for key, values := range query {
		kDir := filepath.Join(rDir, "query", key)
		err = os.Mkdir(kDir, dPerm)
		if err != nil {
			writeServerError(w, 500, fmt.Sprintf("unable to create directory for query parameter %s\n", key))
			return
		}
		for i, value := range values {
			err = os.WriteFile(filepath.Join(kDir, fmt.Sprint(i)), []byte(value), fPerm)
			if err != nil {
				writeServerError(w, 500, fmt.Sprintf("unable to create file for query parameter: %s=%s\n", key, value))
				return
			}
		}
	}

	for key, values := range r.Header {
		err = os.WriteFile(filepath.Join(rDir, "headers", key), []byte(strings.Join(values, ",")), fPerm)
		if err != nil {
			writeServerError(w, 500, fmt.Sprintf("unable to create file for %s header\n", key))
			return
		}
	}

	w.Header().Add("Content-Type", "text/plain")
	w.Write([]byte(dir + "\n")) // TODO
}

func writeServerError(w http.ResponseWriter, status int, message string) {
	w.Header().Add("Content-Type", "text/plain")
	w.WriteHeader(status)
	w.Write([]byte(message))
}

func writeClientError(w http.ResponseWriter, status int, message string) {
	w.Header().Add("Content-Type", "text/plain")
	w.WriteHeader(status)
	w.Write([]byte(message))
}
