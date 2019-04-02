package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/kennygrant/sanitize"

	"github.com/rylio/ytdl"
)

const (
	ApiKey = ""
)

type SearchItemSnippet struct {
	Title        string
	Description  string
	ChannelTitle string
}

type SearchItemID struct {
	VideoID string
}

type SearchResultItem struct {
	ID      SearchItemID
	Snippet SearchItemSnippet
}

type SearchResult struct {
	Kind  string
	Etag  string
	Items []SearchResultItem
}

type VideoResultItemContentDetails struct {
	Duration string
}

type VideoResultItemStatistics struct {
	ViewCount    string
	LikeCount    string
	DislikeCount string
}

type VideoResultItem struct {
	ContentDetails VideoResultItemContentDetails
	Statistics     VideoResultItemStatistics
}

type VideoResult struct {
	Items []VideoResultItem
}

type VideoComplete struct {
	Title        string
	Description  string
	ChannelTitle string
	Duration     string
	Views        string
	LikeRatio    float32
}

func getResponseBody(url string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	return body
}

func search(term string) SearchResult {
	urlStr := "https://www.googleapis.com/youtube/v3/search?part=snippet&maxResults=10&type=video&q=" + url.QueryEscape(term) + "&key=" + ApiKey
	body := getResponseBody(urlStr)
	var ytResults SearchResult
	jsonErr := json.Unmarshal(body, &ytResults)
	if jsonErr != nil {
		log.Fatal(jsonErr)
	}
	return ytResults
}

func searchVideos(ids []string) VideoResult {
	body := getResponseBody("https://www.googleapis.com/youtube/v3/videos?id=" + strings.Join(ids, ",") + "&part=contentDetails,statistics&key=AIzaSyBIehUHoPCo9DcbWV_iKQCUwR9KDQldwy0")
	var result VideoResult
	jsonErr := json.Unmarshal(body, &result)
	if jsonErr != nil {
		log.Fatal(jsonErr)
	}
	return result
}

func main() {
	if len(ApiKey) == 0 {
		fmt.Println("No API key found")
		return
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter search query: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		log.Fatal(err)
	}
	searchResult := search(strings.TrimSpace(input))
	if len(searchResult.Items) == 0 {
		fmt.Println("No videos found for that query.")
		return
	}
	var videoIds []string
	for _, searchResultItem := range searchResult.Items {
		videoIds = append(videoIds, searchResultItem.ID.VideoID)
	}
	videoResult := searchVideos(videoIds)
	aggregated := make(map[string]VideoComplete)
	for searchResultIndex, searchResultItem := range searchResult.Items {
		videoResultItem := videoResult.Items[searchResultIndex]
		likes, err := strconv.ParseFloat(videoResultItem.Statistics.LikeCount, 32)
		if err != nil {
			log.Fatal(err)
		}
		dislikes, err := strconv.ParseFloat(videoResultItem.Statistics.DislikeCount, 32)
		if err != nil {
			log.Fatal(err)
		}
		aggregated[searchResultItem.ID.VideoID] = VideoComplete{
			searchResultItem.Snippet.Title,
			searchResultItem.Snippet.Description,
			searchResultItem.Snippet.ChannelTitle,
			videoResultItem.ContentDetails.Duration,
			videoResultItem.Statistics.ViewCount,
			float32(likes / (likes + dislikes)),
		}
	}

	consoleOutput := "\n--SEARCH RESULTS--\n\n"
	for index, elem := range searchResult.Items {
		id := elem.ID.VideoID
		consoleOutput += fmt.Sprintf("%d) %s (%s)\nChannel: %s\nViews: %s\nLikes: %.2f%%\n\n", index+1, elem.Snippet.Title, aggregated[id].Duration, elem.Snippet.ChannelTitle, aggregated[id].Views, aggregated[id].LikeRatio*100)
	}
	fmt.Println(consoleOutput)
	fmt.Print("Select a Video: ")
	var selection int
	for {
		if input, err := reader.ReadString('\n'); err == nil {
			if num, err := strconv.Atoi(strings.TrimSpace(input)); err == nil {
				if num <= len(searchResult.Items) && num > 0 {
					selection = num
					break
				}
			}
		}
		fmt.Print("Invalid selection, try again: ")
	}

	fmt.Println("Fetching info...")
	vid, err := ytdl.GetVideoInfoFromID(searchResult.Items[selection-1].ID.VideoID)
	if err != nil {
		fmt.Println("Failed to get video info")
		return
	}

	consoleOutput = "\n--AUDIO FORMATS--\n\n"
	var audioFormats []ytdl.Format
	bestFormat := vid.Formats.Best("audbr")[0]
	for _, format := range vid.Formats {
		if len(format.AudioEncoding) > 0 {
			audioFormats = append(audioFormats, format)
			suffix := ""
			if format.AudioEncoding == bestFormat.AudioEncoding && format.AudioBitrate == bestFormat.AudioBitrate {
				suffix = " [BEST]"
			}
			consoleOutput += fmt.Sprintf("%d) Encoding: %s, Bitrate: %d, Extension: %s%s\n", len(audioFormats), format.AudioEncoding, format.AudioBitrate, format.Extension, suffix)
		}
	}

	fmt.Println(consoleOutput)
	fmt.Print("Select a audio format by typing the nubmer, or type \"best\" for the best audio, \"worst\" for the worst audio, or \"fastest\" to download the full video and then convert to mp3 audio with ffmpeg (fastest method): ")

	var formatSelection int
	var selectedFormat ytdl.Format

	for {
		if input, err := reader.ReadString('\n'); err == nil {
			trimmedInput := strings.TrimSpace(input)
			if trimmedInput == "best" {
				formatSelection = -1
				break
			} else if trimmedInput == "fastest" {
				formatSelection = -2
				break
			}
			if num, err := strconv.Atoi(trimmedInput); err == nil {
				if num <= len(audioFormats) && num > 0 {
					formatSelection = num
					break
				}
			}
		}
		fmt.Print("Invalid selection, try again: ")
	}

	if formatSelection == -1 {
		selectedFormat = bestFormat
	} else if formatSelection == -2 {
		selectedFormat = vid.Formats[0]
	} else {
		selectedFormat = audioFormats[formatSelection]
	}

	baseFileName := sanitize.BaseName(vid.Title)
	fullfileName := baseFileName + "." + selectedFormat.Extension
	file, err := os.Create(fullfileName)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	cmd := exec.Command("ytdl", "-f", "itag:"+strconv.Itoa(selectedFormat.Itag), "-o", fullfileName, vid.ID)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	runErr := cmd.Run()
	if runErr != nil {
		log.Fatal(err)
	}
	if formatSelection == -2 {
		bitrate := "192"
		if selectedFormat.AudioBitrate != 0 {
			bitrate = strconv.Itoa(selectedFormat.AudioBitrate)
		}

		fmt.Printf("\nConverting to mp3 with bitrate %sk... \n\n", bitrate)
		cmd := exec.Command("ffmpeg", "-i", fullfileName, "-f", "mp3", "-b:a", bitrate+"k", "-vn", baseFileName+".mp3")
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		runErr := cmd.Run()
		if runErr != nil {
			log.Fatal(err)
		}
		if err = os.Remove(fullfileName); err != nil {
			log.Fatal(err)
		}

	} else {
		fmt.Println("Done")
	}

}
