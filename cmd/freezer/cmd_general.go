// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/tbogdala/filefreezer/cmd/freezer/models"

	"encoding/json"
	"io/ioutil"
)

// authenticate will use a HTTP call to authenticate the user
// and return the JWT token string.
func (s *commandState) authenticate(hostURI, username, password string) error {
	// get the http client to use for the connection
	client, err := getHTTPClient()
	if err != nil {
		return err
	}

	// Build and perform the request
	target := fmt.Sprintf("%s/api/users/login", hostURI)
	resp, err := client.PostForm(target, url.Values{
		"user":     {username},
		"password": {password},
	})
	if err != nil {
		if resp != nil {
			return fmt.Errorf("Failed to make the HTTP POST request to %s (status: %s): %v", target, resp.Status, err)
		}
		return fmt.Errorf("Failed to make the HTTP POST request to %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Failed to read the response body from %s: %v", target, err)
	}

	// check the status code to ensure the success of the call
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Failed to make the HTTP POST request to %s (status: %s): %v", target, resp.Status, string(body))
	}

	// get the response by deserializing the JSON
	var userLogin models.UserLoginResponse
	err = json.Unmarshal(body, &userLogin)
	if err != nil {
		return fmt.Errorf("Poorly formatted response to %s: %v", target, err)
	}

	// authentication was successful so update the command state
	s.hostURI = hostURI
	s.authToken = userLogin.Token
	s.cryptoHash = userLogin.CryptoHash
	s.serverCapabilities = userLogin.Capabilities

	return nil
}

// getHttpClient returns a new http Client object set to work with TLS if keys are provided
// on the command line or plain http otherwise.
func getHTTPClient() (*http.Client, error) {
	var client *http.Client
	if *flagTLSCrt != "" && *flagTLSKey != "" {
		cert, err := tls.LoadX509KeyPair(*flagTLSCrt, *flagTLSKey)
		if err != nil {
			return nil, fmt.Errorf("unable to load cert: %v", err)
		}

		xpool := x509.NewCertPool()
		tlsConfig := &tls.Config{
			RootCAs:      xpool,
			Certificates: []tls.Certificate{cert},
		}
		//tlsConfig.BuildNameToCertificate()
		transport := &http.Transport{TLSClientConfig: tlsConfig}
		client = &http.Client{Transport: transport}

		// Load our trusted certificate path
		certPath := *flagTLSCrt
		pemData, err := ioutil.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to load the certificate file %s: %v", certPath, err)
		}
		ok := tlsConfig.RootCAs.AppendCertsFromPEM(pemData)
		if !ok {
			return nil, fmt.Errorf("couldn't load PEM data for HTTPS client")
		}
	} else {
		client = &http.Client{}
	}

	return client, nil
}

// buildAuthRequest builds a http client and request with the authorization header and token attached.
func buildAuthRequest(target string, method string, token string, bodyBytes []byte) (*http.Client, *http.Request, error) {
	// Load client cert
	client, err := getHTTPClient()
	if err != nil {
		return nil, nil, err
	}

	var req *http.Request
	if bodyBytes != nil {
		req, _ = http.NewRequest(method, target, bytes.NewBuffer(bodyBytes))
	} else {
		req, _ = http.NewRequest(method, target, nil)
	}
	req.Header.Add("Authorization", "Bearer "+token)
	return client, req, nil
}

// runAuthRequest will build the http client and request then get the response and read
// the body into a byte array. If reqBody is a []byte array, no transformation is done,
// but if it's another type than it gets marshalled to a text JSON object.
func runAuthRequest(target string, method string, token string, reqBody interface{}) ([]byte, error) {
	// serialize the reqBody object if one was passed in
	var err error
	var reqBodyIsByteSlice bool
	var reqBytes []byte
	if reqBody != nil {
		reqBytes, reqBodyIsByteSlice = reqBody.([]byte)
		if !reqBodyIsByteSlice {
			reqBytes, err = json.Marshal(reqBody)
			if err != nil {
				return nil, fmt.Errorf("Failed to JSON serialize the data object passed in: %v", err)
			}
		}
	}

	client, req, err := buildAuthRequest(target, method, token, reqBytes)
	if err != nil {
		return nil, err
	}

	// set the header if a JSON object is being sent
	if reqBytes != nil && !reqBodyIsByteSlice {
		req.Header.Set("Content-Type", "application/json")
	}

	// perform the request and read the response body
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Failed to make the HTTP %s request to %s (status: %s): %v", method, target, resp.Status, err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read the response body from %s: %v", target, err)
	}

	// check the status code to ensure the success of the call
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to make the HTTP %s request to %s (status: %s): %v", method, target, resp.Status, string(body))
	}

	return body, nil
}

type eachChunkFunc func(chunkNumber int, chunk []byte) (bool, error)

func forEachChunk(chunkSize int, filename string, localChunkCount int, eachFunc eachChunkFunc) error {
	// open the local file and create a chunk sized buffer
	buffer := make([]byte, chunkSize)
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("Failed to open the file %s: %v", filename, err)
	}
	defer f.Close()

	// with the chunk list, lets make sure that each chunk locally has the same hash
	for i := 0; i < localChunkCount; i++ {
		readCount, err := io.ReadAtLeast(f, buffer, chunkSize)
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				// if we don't fill the buffer and we're not on the last chunk, the files are different
				if i+1 != localChunkCount {
					return fmt.Errorf("nexpeced EOF while reading the file %s", filename)
				}
			} else {
				return fmt.Errorf("an error occured while reading %d bytes from the file %s: %v", readCount, filename, err)
			}
		}
		clampedBuffer := buffer[:readCount]

		// call the supplied callback and break the loop if false is returned
		contLoop, err := eachFunc(i, clampedBuffer)
		if err != nil {
			return err
		}
		if !contLoop {
			break
		}
	}

	return nil
}
