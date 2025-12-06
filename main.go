package main

import (
	"sync"
    "fmt"
	"time"
    "strconv"
	"flag"

	rand "math/rand"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	overflow "github.com/JohnCGriffin/overflow"
	i64 "github.com/adam-lavrik/go-imath/i64"
)

type CountdownTimer struct {
	sync.Mutex
	seconds uint64
	maxSeconds uint64
}

var gCountdownTimer = CountdownTimer { sync.Mutex {}, 0, 2 * 60 * 60 }
var timeTopic *string
var cmdTopic *string
var mqttClient mqtt.Client
var defaultAddedSecondsDuration int64 = 30 * 60


func SetLatchOff() {
	t := mqttClient.Publish(*cmdTopic, 0, false, "OFF")
	go func() {
		_ = t.Wait()
		if t.Error() != nil {
			fmt.Printf("%s: %s\n", time.Now().Format(time.RFC850), t.Error())
		}
	}()
}

func SetLatchOn() {
	t := mqttClient.Publish(*cmdTopic, 0, false, "ON")
	go func() {
		_ = t.Wait()
		if t.Error() != nil {
			fmt.Printf("%s: %s\n", time.Now().Format(time.RFC850), t.Error())
		}
	}()
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

func changeTime(seconds int64) {
	gCountdownTimer.Lock()
	defer gCountdownTimer.Unlock()
	secondsOfTimerBeforeChanging := gCountdownTimer.seconds
	if seconds < 0 {
		secondsAbs := i64.Abs(seconds)
		if uint64(secondsAbs) > gCountdownTimer.seconds {
			gCountdownTimer.seconds = 0
			SetLatchOff()
		} else {
			gCountdownTimer.seconds = uint64(int64(gCountdownTimer.seconds) - secondsAbs)
		}
		fmt.Printf("%s: Subtracted %d seconds so timer is now at %d, was at %d seconds\n", time.Now().Format(time.RFC850), secondsAbs, gCountdownTimer.seconds, secondsOfTimerBeforeChanging)
	} else {
		gCountdownTimer.seconds = uint64(int64(gCountdownTimer.seconds) + seconds)
		if (gCountdownTimer.maxSeconds <= gCountdownTimer.seconds) {
			fmt.Printf("%s: Timer truncated to %d seconds\n", time.Now().Format(time.RFC850), gCountdownTimer.maxSeconds)
			gCountdownTimer.seconds = gCountdownTimer.maxSeconds
		}
		fmt.Printf("%s: Added %d seconds so timer is now at %d, was at %d seconds\n", time.Now().Format(time.RFC850), seconds, gCountdownTimer.seconds, secondsOfTimerBeforeChanging)
		SetLatchOn()
	}
}

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	// There are 2 valid inputs - empty message (zero bytes) or a textual ASCII numbers-only message
	if len(msg.Payload()) == 0 {
		fmt.Printf("%s: Received empty message from topic: %s\n", time.Now().Format(time.RFC850), msg.Topic())
		changeTime(defaultAddedSecondsDuration)
		return
	}
	
	fmt.Printf("%s: Received message: %s from topic: %s\n", time.Now().Format(time.RFC850), msg.Payload(), msg.Topic())
	// For "a textual ASCII numbers-only message" - we will try to parse it into a valid number, and if parsing was not
	// succesful then we will simply ignore the message.
	changedTimeInMinutes, err := strconv.ParseInt(string(msg.Payload()), 10, 64)
    if err != nil {
		return
    }
	changedTimeInSeconds, ok := overflow.Mul64(changedTimeInMinutes, 60)
	if !ok {
		return
	}
	changeTime(changedTimeInSeconds)
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
    fmt.Printf("%s: Connected to MQTT broker\n", time.Now().Format(time.RFC850))
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
    fmt.Printf("%s: Connection lost from MQTT broker: %v\n, aborting!", time.Now().Format(time.RFC850), err)
	SetLatchOff()
	panic("Connection lost from MQTT broker")
}

func main() {
	broker := flag.String("mqtt-broker", "127.0.0.1", "MQTT Broker hostname/IP")
	port := flag.Uint64("mqtt-port", 1883, "MQTT Broker Port")
	timeTopic = flag.String("mqtt-duration-topic", "test-topic", "MQTT Topic to subscribe for receiving start/add durations")
	cmdTopic = flag.String("mqtt-switch-cmd-topic", "test-cmd-topic", "MQTT Topic to send ON/OFF switch commandss")
	username := flag.String("mqtt-username", "", "MQTT Broker username")
	password := flag.String("mqtt-password", "", "MQTT Broker password")
	flag.Int64Var(&defaultAddedSecondsDuration, "default-added-seconds-duration", 30 * 60, "Default Added Seconds Duration on empty MQTT message")
    flag.Parse()

	if (*port > uint64(65535)) {
		panic("Invalid port number")
	}

	if (defaultAddedSecondsDuration < 0) {
		panic("Invalid default added seconds duration")
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
	opts.SetPingTimeout(10 * time.Second)
	opts.SetKeepAlive(10 * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(10 * time.Second)
    opts.OnConnect = connectHandler
    opts.OnConnectionLost = connectLostHandler
    mqttClient = mqtt.NewClient(opts)
    if token := mqttClient.Connect(); !token.WaitTimeout(10 * time.Second) || token.Error() != nil {
		if token.Error() != nil {
			panic(token.Error())
		}
        panic("Timeout when initialzing a connection to MQTT broker!\n")
  	}

	token := mqttClient.Subscribe(*timeTopic, 1, nil)
	if !token.WaitTimeout(10 * time.Second) {
		panic("Timeout when subscribing to topic!\n")
	}
	fmt.Printf("%s: Subscribed to topic %s\n", time.Now().Format(time.RFC850), *timeTopic)

	go countdown()
	select {} // block forever
}
