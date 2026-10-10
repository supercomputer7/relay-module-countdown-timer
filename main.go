package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	overflow "github.com/JohnCGriffin/overflow"
	i64 "github.com/adam-lavrik/go-imath/i64"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	rand "math/rand"
)

type CountdownTimer struct {
	sync.Mutex
	seconds    uint64
	maxSeconds uint64
}

var gCountdownTimer = CountdownTimer{sync.Mutex{}, 0, 2 * 60 * 60}
var timeTopic *string
var cmdTopic *string
var mqttCmdOn *string
var mqttCmdOff *string
var mqttClient mqtt.Client
var defaultAddedSecondsDuration int64 = 30 * 60

func SetLatchOff() {
	t := mqttClient.Publish(*cmdTopic, 0, false, *mqttCmdOff)
	go func() {
		_ = t.Wait()
		if t.Error() != nil {
			fmt.Printf("%s: %s\n", time.Now().Format(time.RFC850), t.Error())
		}
	}()
}

func SetLatchOn() {
	t := mqttClient.Publish(*cmdTopic, 0, false, *mqttCmdOn)
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
	if gCountdownTimer.seconds == 0 {
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
		if gCountdownTimer.maxSeconds <= gCountdownTimer.seconds {
			fmt.Printf("%s: Timer truncated to %d seconds\n", time.Now().Format(time.RFC850), gCountdownTimer.maxSeconds)
			gCountdownTimer.seconds = gCountdownTimer.maxSeconds
		}
		fmt.Printf("%s: Added %d seconds so timer is now at %d, was at %d seconds\n", time.Now().Format(time.RFC850), seconds, gCountdownTimer.seconds, secondsOfTimerBeforeChanging)
		SetLatchOn()
	}
}

func handlePayload(msg string) {
	changedTimeInMinutes, err := strconv.ParseInt(msg, 10, 64)
	if err != nil {
		return
	}
	changedTimeInSeconds, ok := overflow.Mul64(changedTimeInMinutes, 60)
	if !ok {
		return
	}
	changeTime(changedTimeInSeconds)
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
	handlePayload(string(msg.Payload()))
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	fmt.Printf("%s: Connected to MQTT broker\n", time.Now().Format(time.RFC850))
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	fmt.Printf("%s: Connection lost from MQTT broker: %v\n, aborting!", time.Now().Format(time.RFC850), err)
	SetLatchOff()
	panic("Connection lost from MQTT broker")
}

func servePage(w http.ResponseWriter, r *http.Request) {
	html := `
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>IoT Push button control</title>
<style>
    body { font-family: Arial, sans-serif; text-align: center; background: #f0f0f0; }
    h1 { margin-top: 50px; }
    .buttons { width: 90%; max-width: 400px; margin: 50px auto; }
    button {
        display: block;
        width: 100%;
        padding: 25px 0;
        font-size: 1.8rem;
        margin-bottom: 20px;
        border: none;
        border-radius: 10px;
        cursor: pointer;
        background-color: #ccc;
    }
    button:active { background-color: #aaa; }
</style>
</head>
<body>
<h1>IoT control buttons</h1>
<div class="buttons">
    <button id="btn1">Add 30 minutes</button>
    <button id="btn2">Power off now</button>
</div>

<script>
var btn1 = document.getElementById('btn1');
var btn2 = document.getElementById('btn2');

btn1.onclick = function() { sendMessage('30', btn1); };
btn2.onclick = function() { sendMessage('-1800', btn2); };

function sendMessage(msg, button) {
    button.disabled = true;
    var xhr = new XMLHttpRequest();
    xhr.open('GET', '/send?msg=' + encodeURIComponent(msg), true);
    xhr.onreadystatechange = function() {
        if(xhr.readyState === 4) {
            console.log(xhr.responseText);
            setTimeout(function() { button.disabled = false; }, 300);
        }
    };
    xhr.send();
}
</script>
</body>
</html>
`
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, html)
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get("msg")
	if msg == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Missing msg parameter")
		return
	}

	// this is not the best practice but we need to make sure the relay is not switched on and off rapidly
	time.Sleep(500 * time.Millisecond)
	handlePayload(msg)
	fmt.Fprintf(w, "Message sent: %s", msg)
}

func main() {
	broker := flag.String("mqtt-broker", "127.0.0.1", "MQTT Broker hostname/IP")
	port := flag.Uint64("mqtt-port", 1883, "MQTT Broker Port")
	httpPort := flag.Uint64("http-port", 8080, "HTTP Port")
	timeTopic = flag.String("mqtt-duration-topic", "test-topic", "MQTT Topic to subscribe for receiving start/add durations")
	cmdTopic = flag.String("mqtt-switch-cmd-topic", "test-cmd-topic", "MQTT Topic to send ON/OFF switch commands")
	cmdOn := flag.String("mqtt-switch-on-cmd", "ON", "MQTT string for ON switch command")
	cmdOff := flag.String("mqtt-switch-off-cmd", "OFF", "MQTT string for OFF switch command")
	username := flag.String("mqtt-username", "", "MQTT Broker username")
	password := flag.String("mqtt-password", "", "MQTT Broker password")
	flag.Int64Var(&defaultAddedSecondsDuration, "default-added-seconds-duration", 30*60, "Default Added Seconds Duration on empty MQTT message")
	flag.Parse()

	if *port > uint64(65535) {
		panic("Invalid port number")
	}

	if defaultAddedSecondsDuration < 0 {
		panic("Invalid default added seconds duration")
	}

	// This is probably unsafe for anything serious, but since we use
	// rand for generating somewhat-random client ID, this is probably OK.
	rand.Seed(time.Now().UnixNano())

	clientId := fmt.Sprintf("mqtt-relay-module-timer-%d", rand.Int())
	fmt.Printf("Connecting to %s:%d, with client id of %s\n", *broker, *port, clientId)

	mqttCmdOn = cmdOn
	mqttCmdOff = cmdOff

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
	if token := mqttClient.Connect(); !token.WaitTimeout(10*time.Second) || token.Error() != nil {
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

	// Serve HTML page
	http.HandleFunc("/", servePage)
	// Handle button presses
	http.HandleFunc("/send", handleSend)

	go func() {
		fmt.Printf("Starting HTTP server at :%d\n", *httpPort)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPort), nil); err != nil {
			log.Fatal(err)
		}
	}()

	select {} // block forever
}
