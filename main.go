package main

import (
	"sync"
    "fmt"
	"time"
    "strconv"
	"flag"

	rand "math/rand"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	gpiocdev "github.com/warthog618/go-gpiocdev"
)

type CountdownTimer struct {
	sync.Mutex
	seconds uint64
	maxSeconds uint64
}

var gCountdownTimer = CountdownTimer { sync.Mutex {}, 0, 2 * 60 * 60 }
var gpiochipLine *gpiocdev.Line
var gpioChipLineActiveHigh bool = true
var defaultAddedSecondsDuration uint64 = 30 * 60


func SetLatchOff() {
	if gpioChipLineActiveHigh {
		gpiochipLine.SetValue(0)
	} else {
		gpiochipLine.SetValue(1)
	}
}

func SetLatchOn() {
	if gpioChipLineActiveHigh {
		gpiochipLine.SetValue(1)
	} else {
		gpiochipLine.SetValue(0)
	}
}

func decreaseOneSecond() {
	defer gCountdownTimer.Unlock()
	
	gCountdownTimer.Lock()
	if gCountdownTimer.seconds == 0 {
		return
	}
	
	gCountdownTimer.seconds = gCountdownTimer.seconds - 1
	if (gCountdownTimer.seconds == 0) {
		fmt.Printf("%s: Timer now at 0, will shut off latch\n", time.Now().Format(time.RFC850))
		SetLatchOff()
	}
}

func countdown() {
	SetLatchOff()
	for {
		time.Sleep(1 * time.Second)
		decreaseOneSecond()
	}
}

func addMoreTime(seconds uint64) {
	gCountdownTimer.Lock()
	secondsOfTimerBeforeAdding := gCountdownTimer.seconds
	gCountdownTimer.seconds = gCountdownTimer.seconds + seconds
	if (gCountdownTimer.maxSeconds <= gCountdownTimer.seconds) {
		fmt.Printf("%s: Timer truncated to %d seconds\n", time.Now().Format(time.RFC850), gCountdownTimer.maxSeconds)
		gCountdownTimer.seconds = gCountdownTimer.maxSeconds
	}
	fmt.Printf("%s: Added %d seconds so timer is now at %d, was at %d seconds\n", time.Now().Format(time.RFC850), seconds, gCountdownTimer.seconds, secondsOfTimerBeforeAdding)
	SetLatchOn()
	gCountdownTimer.Unlock()
}

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	// There are 2 valid inputs - empty message (zero bytes) or a textual ASCII numbers-only message
	if len(msg.Payload()) == 0 {
		fmt.Printf("%s: Received empty message from topic: %s\n", time.Now().Format(time.RFC850), msg.Topic())
		addMoreTime(defaultAddedSecondsDuration)
		return
	}
	
	fmt.Printf("%s: Received message: %s from topic: %s\n", time.Now().Format(time.RFC850), msg.Payload(), msg.Topic())
	// For "a textual ASCII numbers-only message" - we will try to parse it into a valid number, and if parsing was not
	// succesful then we will simply ignore the message.
	addedTimeInSeconds, err := strconv.ParseUint(string(msg.Payload()), 10, 64)
    if err != nil || addedTimeInSeconds < 0 {
		return
    }
	addMoreTime(addedTimeInSeconds)
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
    fmt.Printf("%s: Connected to MQTT broker\n", time.Now().Format(time.RFC850))
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
    fmt.Printf("%s: Connection lost from MQTT broker: %v\n", time.Now().Format(time.RFC850), err)
}

func main() {
	broker := flag.String("mqtt-broker", "127.0.0.1", "MQTT Broker hostname/IP")
	port := flag.Uint64("mqtt-port", 1883, "MQTT Broker Port")
	topic := flag.String("mqtt-topic", "test-topic", "MQTT Topic to subscribe")
	username := flag.String("mqtt-username", "", "MQTT Broker username")
	password := flag.String("mqtt-password", "", "MQTT Broker password")
	gpioChipName := flag.String("gpio-chip", "gpiochip0", "GPIO Chip Name")
	gpioChipLineNumber := flag.Int("gpio-chip-line", 1, "GPIO Chip Line for setting latch")
	flag.BoolVar(&gpioChipLineActiveHigh, "active-high", false, "Latch is Active High triggered")
	flag.Uint64Var(&defaultAddedSecondsDuration, "default-added-seconds-duration", 30 * 60, "Default Added Seconds Duration on empty MQTT message")
    flag.Parse()

	if (*port > uint64(65535)) {
		panic("Invalid port number")
	}

	if (*gpioChipLineNumber < 0) {
		panic("Invalid GPIO line number")
	}

	// This is probably unsafe for anything serious, but since we use
	// rand for generating somewhat-random client ID, this is probably OK.
	rand.Seed(time.Now().UnixNano())

	clientId := fmt.Sprintf("mqtt-relay-module-timer-%d", rand.Int())
	fmt.Printf("Connecting to %s:%d, with client id of %s\n", *broker, *port, clientId)

    opts := mqtt.NewClientOptions()
    opts.AddBroker(fmt.Sprintf("tcp://%s:%d", *broker, *port))
    opts.SetClientID(clientId)
    opts.SetUsername(*username)
    opts.SetPassword(*password)
    opts.SetDefaultPublishHandler(messagePubHandler)
    opts.OnConnect = connectHandler
    opts.OnConnectionLost = connectLostHandler
    client := mqtt.NewClient(opts)
    if token := client.Connect(); !token.WaitTimeout(10 * time.Second) || token.Error() != nil {
		if token.Error() != nil {
			panic(token.Error())
		}
        panic("Timeout when initialzing a connection to MQTT broker!\n")
  	}

	token := client.Subscribe(*topic, 1, nil)
	if !token.WaitTimeout(10 * time.Second) {
		panic("Timeout when subscribing to topic!\n")
	}
	fmt.Printf("%s: Subscribed to topic %s\n", time.Now().Format(time.RFC850), *topic)

	line, err := gpiocdev.RequestLine(*gpioChipName, *gpioChipLineNumber, gpiocdev.AsOutput(1))
	if err != nil {
		panic(err)
	}

	gpiochipLine = line

	go countdown()
	select {} // block forever
}
