package main

import (
	"embed"
	"fmt"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imroc/req"
	"github.com/mattn/go-tty"
	"io/ioutil"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type SugarLevel struct {
	ID         string  `json:"_id"`
	Device     string  `json:"device"`
	Date       int64   `json:"date"`
	DateString string  `json:"dateString"`
	SugarLevel int     `json:"sgv"`
	Delta      float64 `json:"delta"`
	Direction  string  `json:"direction"`
	Type       string  `json:"type"`
	Filtered   int     `json:"filtered"`
	Unfiltered int     `json:"unfiltered"`
	Rssi       int     `json:"rssi"`
	Noise      int     `json:"noise"`
	SysTime    string  `json:"sysTime"`
}

var sugarDangerous bool
var sugarLevel float64
var snoozedTill time.Time
var ctrl beep.Ctrl
var measureTime time.Time

//go:embed assets/*.mp3
var f embed.FS
//go:embed config.txt
var configFS embed.FS

func playSugar() {

	f1, err := f.Open("assets/" + strconv.FormatFloat(sugarLevel, 'f', 1, 64) + ".mp3")
	if err != nil {
		log.Fatal(err)
	}

	streamer1, _, err := mp3.Decode(f1)
	if err != nil {
		log.Fatal(err)
	}
	defer streamer1.Close()

	done := make(chan bool)

	speaker.Lock()
	paused := ctrl.Paused
	ctrl.Paused = true
	speaker.Unlock()

	speaker.Play(beep.Seq(streamer1, beep.Callback(func() {
		done <- true
	})))

	<-done
	speaker.Lock()
	ctrl.Paused = paused
	speaker.Unlock()
}

func main() {
	fmt.Println("asdasd")
	f, err := f.Open("assets/ring.mp3")
	if err != nil {
		log.Fatal(err)
	}

	streamer, format, err := mp3.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	defer streamer.Close()
	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	buffer := beep.NewBuffer(format)
	buffer.Append(streamer)
	streamer.Close()

	bufferStreamer := buffer.Streamer(0, buffer.Len())
	ctrl = beep.Ctrl{Streamer: beep.Loop(-1, bufferStreamer), Paused: true}
	speaker.Play(&ctrl)

	//go runTelegramBot()
	go refreshSugarLevel()

	tty, err := tty.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer tty.Close()

	for {
		r, err := tty.ReadRune()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("Key press => " + string(r))
		switch r {
		case '1':
			{
				delta := time.Minute * 5
				snoozedTill = time.Now().Add(delta)
				fmt.Println("snoozed for 5 minutes")
				startSpeakerIfNeeded()
				go func() {
					time.Sleep(delta)
					startSpeakerIfNeeded()
				}()
				bufferStreamer.Seek(0)
			}
		case '2':
			{
				delta := time.Minute * 30
				snoozedTill = time.Now().Add(delta)
				fmt.Println("snoozed for 15 minutes")
				startSpeakerIfNeeded()
				go func() {
					time.Sleep(delta)
					startSpeakerIfNeeded()
				}()
				bufferStreamer.Seek(0)
			}
		case '3':
			{
				playSugar()
			}
		}

	}
}

func refreshSugarLevel() {
	for {
		time.Sleep(10 * time.Second)
		resp, err := req.Get(os.Getenv("NIGHTSCOUT") + "/api/v1/entries/current.json")
		if err != nil {
			log.Println("err", err.Error())
			continue
		}
		var levels []SugarLevel
		err = resp.ToJSON(&levels)
		if err != nil {
			log.Println("err", err.Error())
			continue
		}
		if len(levels) == 0 {
			log.Println("empty array")
			continue
		}

		level := levels[0]
		sugarLevel = float64(level.SugarLevel) * 0.0555
		measureTime, _ = time.Parse("2006-01-02T15:04:05-0700", level.DateString)
		delta := level.Delta * 0.0555
		min, max := getMinMax()
		fmt.Println(math.Round(sugarLevel*10)/10, math.Round(delta*10)/10, measureTime, snoozedTill, min, max, sugarDangerous)
		startSpeakerIfNeeded()
	}
}

func getMinMax() (float64, float64) {
	var min, max float64
	b, err := ioutil.ReadFile("config.txt") // just pass the file name
	if err != nil {
		fmt.Print(err)
		b, err = configFS.ReadFile("config.txt")
		if err != nil {
			fmt.Print(err)
			return 5, 9
		}
	}

	min = 100

	for _, s := range strings.Split(string(b), "\n") {
		i, err := strconv.ParseFloat(s, 64)
		if err != nil {
			fmt.Print(err)
		}
		if i < min {
			min = i
		}
		if i > max {
			max = i
		}
	}
	return min, max
}

func startSpeakerIfNeeded() {
	min, max := getMinMax()
	sugarDangerous = sugarLevel > max || sugarLevel < min || time.Since(measureTime) > time.Minute*10
	speaker.Lock()
	ctrl.Paused = !(sugarDangerous && time.Now().After(snoozedTill))
	speaker.Unlock()
}


func runTelegramBot() {
	bot, err := tgbotapi.NewBotAPI(os.Getenv("TGTOKEN"))
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil { // ignore any non-Message Updates
			continue
		}

		log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

		d1 := []byte(update.Message.Text)
    	err := os.WriteFile("config.txt", d1, 0644)
		if err != nil {
			log.Panic(err)
		}
		
		// msg := tgbotapi.NewMessage(update.Message.Chat.ID, update.Message.Text)
		// msg.ReplyToMessageID = update.Message.MessageID

		// bot.Send(msg)
	}
}