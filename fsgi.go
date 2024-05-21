package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	opts, err := parseCommandLine(os.Args)
	if err != nil {
		log.Fatal(err)
	}

	workDir, err := os.MkdirTemp("", "")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	s := http.Server{
		Addr: opts.Listen,
		Handler: &requestHandler{
			Command: opts.Command,
			WorkDir: workDir,
		},
	}
	log.Println("going to listen on", s.Addr)
	log.Fatal(s.ListenAndServe())
}

func parseCommandLine(args []string) (options, error) {
	var opts options
	usage := "usage: <listen> -- <command> [<arg> ...]"
	// <server> <listen> "--" <command> [<arg> ... ]
	if len(args) < 4 {
		return opts, fmt.Errorf("wrong number of arguments\n\n%s", usage)
	}
	if args[2] != "--" {
		return opts, fmt.Errorf("use \"--\" to separate listen interface from command\n\n%s", usage)
	}
	opts.Listen = args[1]
	opts.Command = args[3:]
	return opts, nil
}

type options struct {
	Listen  string
	Command []string
}

type requestHandler struct {
	Command []string
	WorkDir string
}

func (h *requestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dir, err := os.MkdirTemp(h.WorkDir, "")
	if err != nil {
		writeServerError(w, 500, "unable to create temporary directory\n")
		return
	}
	defer os.RemoveAll(dir)

	// file permission for directories
	var dPerm fs.FileMode = 0750
	// file permission for regular files
	var fPerm fs.FileMode = 0640

	// request directory
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

	body, err := os.OpenFile(filepath.Join(rDir, "body"), os.O_CREATE|os.O_RDWR, fPerm)
	if err != nil {
		writeServerError(w, 500, "unable to create request/body file\n")
		return
	}
	io.Copy(body, r.Body)
	body.Close()

	// The "request/" directory is ready.
	// Create the "response/" directory and its subdirectories, and then invoke
	// the request handling program.

	// response/headers/ directory
	rDir = filepath.Join(dir, "response")
	err = os.MkdirAll(filepath.Join(rDir, "headers"), dPerm)
	if err != nil {
		writeServerError(w, 500, "unable to create response/headers/ directory\n")
		return
	}

	child := exec.Command(h.Command[0], h.Command[1:]...)
	child.Dir = dir
	err = child.Run()
	if err != nil {
		writeServerError(w, 502, fmt.Sprintf("request handler failed: %v\n", err))
		return
	}

	// Examine any files created by the request handling program, and deliver the
	// resulting response.

	// response/status
	var status int
	statusRaw, err := os.ReadFile(filepath.Join(rDir, "status"))
	if err == nil {
		status, err = strconv.Atoi(string(statusRaw))
		if err != nil {
			writeServerError(w, 500, "unable to parse response status\n")
			return
		}
	} else if errors.Is(err, os.ErrNotExist) {
		status = 200
	} else {
		writeServerError(w, 500, "unable to read response/status file\n")
		return
	}

	// response/headers/
	hDir := filepath.Join(rDir, "headers")
	entries, err := os.ReadDir(hDir)
	if err != nil {
		writeServerError(w, 500, "unable to read response/headers/ directory\n")
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if http.CanonicalHeaderKey(name) == "Content-Length" {
			continue
		}
		value, err := os.ReadFile(filepath.Join(hDir, name))
		if err != nil {
			writeServerError(w, 500, fmt.Sprintf("unable to read %s response header\n", name))
			return
		}
		w.Header().Add(name, string(value))
	}

	// response/body
	bodyFile, err := os.Open(filepath.Join(rDir, "body"))
	if err == nil {
		if w.Header().Values("Content-Type") == nil {
			mimeType, err := sniffContentType(bodyFile)
			if err != nil {
				writeServerError(w, 500, fmt.Sprintf("unable to determine Content-Type: %v", err))
				return
			}
			w.Header().Set("Content-Type", mimeType)
		}
		w.WriteHeader(status)
		io.Copy(w, bodyFile)
		bodyFile.Close()
	} else if errors.Is(err, os.ErrNotExist) {
		w.WriteHeader(status)
	} else {
		writeServerError(w, 500, "unable to read response/body file\n")
		return
	}
}

func sniffContentType(body io.ReadSeeker) (string, error) {
	var buf [512]byte
	n, _ := io.ReadFull(body, buf[:])
	mimeType := http.DetectContentType(buf[:n])
	_, err := body.Seek(0, io.SeekStart)
	return mimeType, err
}

func writeServerError(w http.ResponseWriter, status int, message string) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(message))
}

func writeClientError(w http.ResponseWriter, status int, message string) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(message))
}
