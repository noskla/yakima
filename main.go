package main

// go-mediainfo requires libmediainfo0v5 libmediainfo-dev packages to be installed (Ubuntu)

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/zelenin/go-mediainfo"
)

const (
	// PlaybackDirectory is a full or relative path to a directory containing
	// music to be streamed to an Icecast server
	// Note the ending separator
	PlaybackDirectory string = "/home/alis/Music/"
	// Loop between files / start over when all files were already streamed
	Loop bool = true
	// Shuffle files before streaming
	Shuffle bool = false
	// IcecastAddress represents an IP address of the Icecast2 server
	IcecastAddress = "127.0.0.1"
	// IcecastPort represents a port Icecast2 server listens on
	IcecastPort = 8999
	// IcecastUser represents a username of a source to stream with
	IcecastUser = "source"
	// IcecastPassword represents a password for the above user
	IcecastPassword = "1234"
)

// AudioFileQuality represents an audio format and quality information of a type AudioFile
type AudioFileQuality struct {
	bitrate     int
	sampleRate  int
	channelMode string
	format      string
}

// AudioFile represents an audio file in the filesystem
type AudioFile struct {
	filename        string
	path            string
	duration        int
	originalQuality AudioFileQuality
}

var playbackHistory []AudioFile
var icecastInstance net.Conn

// Splits the string containing path with operating system's path separator and
// returns the last value from created array
func getFileNameFromPath(pathToFile string) string {
	path := strings.Split(pathToFile, string(os.PathSeparator))
	return path[len(path)-1]
}

// Reads the audio file and returns an initialized AudioFile object or nil if error occurs
func readAudioFile(pathToFile string) (*AudioFile, bool) {
	file, err := mediainfo.Open(pathToFile)
	if err != nil {
		fmt.Println("Cannot read \"" + pathToFile + "\", does this file exist?")
		return nil, false
	}
	defer file.Close()

	extension := file.Parameter(mediainfo.StreamAudio, 0, "FileExtension")
	bitrate, _ := strconv.Atoi(file.Parameter(mediainfo.StreamAudio, 0, "BitRate"))
	bitrate /= 1000 // Convert bps to kbps
	duration, _ := strconv.Atoi(file.Parameter(mediainfo.StreamAudio, 0, "Duration"))
	duration /= 1000 // Convert ms to seconds
	channels := file.Parameter(mediainfo.StreamAudio, 0, "Channel(s)")
	if channels == "2" {
		channels = "stereo"
	} else {
		channels = "mono"
	}
	samplerate, _ := strconv.Atoi(file.Parameter(mediainfo.StreamAudio, 0, "SampleRate"))

	audio := AudioFile{
		getFileNameFromPath(pathToFile),
		pathToFile,
		duration,
		AudioFileQuality{
			bitrate,
			samplerate,
			channels,
			extension,
		},
	}

	return &audio, true
}

// Start a new socket connection, send HTTP headers,
// wait for 100-Continue HTTP status and return
func beginIcecastConnection(address string) net.Conn {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		fmt.Println("Couldn't establish connection with Icecast.")
		fmt.Println(err.Error())
		os.Exit(2)
	}

	var authCredentials []byte = []byte(IcecastUser + ":" + IcecastPassword)
	var credentialsB64 string = base64.StdEncoding.EncodeToString(authCredentials)

	fmt.Fprintf(conn, "PUT /stream.mp3 HTTP/1.1\r\n")
	fmt.Fprintf(conn, "Host: http://"+address+"\r\n")
	fmt.Fprintf(conn, "Authorization: Basic "+credentialsB64+"\r\n")
	fmt.Fprintf(conn, "User-Agent: Yakima/1.0\r\n")
	fmt.Fprintf(conn, "Accept: */*\r\n")
	fmt.Fprintf(conn, "Transfer-Encoding: chunked\r\n")
	fmt.Fprintf(conn, "Content-Type: audio/mpeg\r\n")
	fmt.Fprintf(conn, "Ice-Public: 1\r\n")
	fmt.Fprintf(conn, "Ice-Genre: Yakima\r\n")
	fmt.Fprintf(conn, "Expect: 100-continue\r\n\r\n")

	buff := make([]byte, 1024)
	conn.Read(buff)

	reply := string(buff)
	if strings.Contains(reply, "HTTP/1.1 100 Continue") {
		return conn
	}

	fmt.Println("Icecast server refused data transfer:")
	fmt.Println(reply)
	os.Exit(3)

	return conn
}

func main() {
	files, err := ioutil.ReadDir(PlaybackDirectory)
	if err != nil {
		fmt.Println("Could not read directory \"" + PlaybackDirectory + "\", perhaps I don't have read access?")
		os.Exit(1)
	}

	icecastFullAddress := IcecastAddress + ":" + strconv.Itoa(IcecastPort)
	icecastInstance = beginIcecastConnection(icecastFullAddress)
	defer icecastInstance.Close()

	var fileIndex int = -1
	var currentFile os.FileInfo
	var currentFileBin *os.File
	for {
		fileIndex++
		if fileIndex == len(files) {
			if Loop {
				fileIndex = -1
				continue
			} else {
				break
			}
		}

		currentFile = files[fileIndex]
		if currentFile.IsDir() {
			continue
		}

		fileData, success := readAudioFile(PlaybackDirectory + currentFile.Name())
		if !success {
			fmt.Println("Error reading " + currentFile.Name())
			fileData = nil
			continue
		}

		fmt.Println("Read " + fileData.filename + " (~" + strconv.Itoa(fileData.duration/60) + " min)")
		cmd := exec.Command("ffmpeg", "-re", "-i", PlaybackDirectory+currentFile.Name(),
			"-f", "mp3", "-c:a", "mp3", "-b:a", "128k", "-")
		wg := sync.WaitGroup{}
		wg.Add(1)
		fmt.Println(cmd.String())

		// FFmpeg
		go func() {
			defer wg.Done()
			cmd.Stdout = icecastInstance
			//cmd.Stderr = os.Stdout
			cmd.Run()
		}()

		wg.Wait()
		currentFileBin.Close()
	}

}
