/*
 * This part is file of VencordInstaller
 * Copyright (c) 2022 Vendicated
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	path "path/filepath"
	"strconv"
	"strings"
	"sync"
)

type GithubRelease struct {
	Name    string `json:"name"`
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

var ReleaseData GithubRelease
var GithubError error
var GithubDoneChan chan bool

var InstalledHash = "None"
var LatestHash = "Unknown"
var IsDevInstall bool

func GetGithubRelease(url string) (*GithubRelease, error) {
	fmt.Println("Fetching", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Failed to create Request", err)
		return nil, err
	}

	req.Header.Set("User-Agent", UserAgent)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("Failed to send Request", err)
		return nil, err
	}

	defer res.Body.Close()

	if res.StatusCode >= 300 {
		err = errors.New(res.Status)
		fmt.Println("Github returned Non-OK status", GithubError)
		return nil, err
	}

	var data GithubRelease

	if err = json.NewDecoder(res.Body).Decode(&data); err != nil {
		fmt.Println("Failed to decode GitHub JSON Response", err)
		return nil, err
	}

	return &data, nil
}

func InitGithubDownloader() {
	GithubDoneChan = make(chan bool, 1)

	IsDevInstall = os.Getenv("VENCORD_DEV_INSTALL") == "1"
	fmt.Println("Is Dev Install: ", IsDevInstall)
	if IsDevInstall {
		GithubDoneChan <- true
		return
	}

	go func() {
		// Make sure UI updates once the request either finished or failed
		defer func() {
			GithubDoneChan <- GithubError == nil
		}()

		data, err := GetGithubRelease(ReleaseUrl)
		if err != nil {
			GithubError = err
			return
		}

		ReleaseData = *data

		i := strings.LastIndex(data.Name, " ") + 1
		LatestHash = data.Name[i:]
		fmt.Println("Finished fetching GitHub Data")
		fmt.Println("Latest hash is", LatestHash, "Local Install is", Ternary(LatestHash == InstalledHash, "up to date!", "outdated!"))
	}()

	// Check hash of installed version if exists
	f, err := os.Open(Patcher)
	if err != nil {
		return
	}
	//goland:noinspection GoUnhandledErrorResult
	defer f.Close()

	fmt.Println("Found existing Vencord Install. Checking for hash...")
	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "// Vencord ") {
			InstalledHash = line[11:]
			fmt.Println("Existing hash is", InstalledHash)
		} else {
			fmt.Println("Didn't find hash")
		}
	}
}

func installLatestBuilds() (retErr error) {
	fmt.Println("Installing latest builds...")

	var wg sync.WaitGroup

	for _, ass := range ReleaseData.Assets {
		if strings.HasPrefix(ass.Name, "patcher.js") ||
			strings.HasPrefix(ass.Name, "preload.js") ||
			strings.HasPrefix(ass.Name, "renderer.js") ||
			strings.HasPrefix(ass.Name, "renderer.css") {
			wg.Add(1)
			ass := ass // Need to do this to not have the variable be overwritten halfway through
			go func() {
				defer wg.Done()
				fmt.Println("Downloading file", ass.Name)

				res, err := http.Get(ass.DownloadURL)
				if err == nil && res.StatusCode >= 300 {
					err = errors.New(res.Status)
				}
				if err != nil {
					fmt.Println("Failed to download", ass.Name+":", err)
					retErr = err
					return
				}
				outFile := path.Join(FilesDir, ass.Name)
				out, err := os.OpenFile(outFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
				if err != nil {
					fmt.Println("Failed to create", outFile+":", err)
					retErr = err
					return
				}
				read, err := io.Copy(out, res.Body)
				if err != nil {
					fmt.Println("Failed to download to", outFile+":", err)
					retErr = err
					return
				}
				contentLength := res.Header.Get("Content-Length")
				expected := strconv.FormatInt(read, 10)
				if expected != contentLength {
					err = errors.New("Unexpected end of input. Content-Length was " + contentLength + ", but I only read " + expected)
					fmt.Println(err)
					retErr = err
					return
				}
			}()
		}
	}

	wg.Wait()
	fmt.Println("Done!")
	_ = FixOwnership(FilesDir)

	InstalledHash = LatestHash
	return
}
