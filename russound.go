package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type event string

const (
	SourceAux = "3"
)

const (
	EventAllOff       = event("AllOff")
	EventKeyPress     = event("KeyPress")
	EventSelectSource = event("SelectSource")
	EventZoneOff      = event("ZoneOff")
)

const (
	KeyPressVolumeDown = "VolumeDown"
	KeyPressVolumeUp   = "VolumeUp"
)

type speakerEvent struct {
	Speaker string
}

var zones = map[string][]int{
	"all":         {1, 2, 3, 4, 5, 6},
	"living area": {3, 4},

	"bedroom":     {1},
	"bathroom":    {2},
	"family room": {3},
	"living room": {3},
	"kitchen":     {4},
	"study":       {5},
	"office":      {5},
	"porch":       {6},
}

func zoneLookup(target string) []int {
	for z, i := range zones {
		if strings.Contains(target, z) {
			return i
		}
	}

	return nil
}

type connection struct {
	remoteIp string
	log      *log.Logger
}

func (c connection) run(cmd string) (string, error) {
	if err := wol(); err != nil {
		return "", err
	}

	conn, err := c.connect()
	if err != nil {
		return "", err
	}

	defer conn.Close()

	conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))

	if n, err := conn.Write([]byte(cmd)); n != len(cmd) {
		return "", errors.New("short write")
	} else if err != nil {
		return "", err
	}

	response := make([]byte, 1024)
	conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	if n, err := conn.Read(response); err != nil {
		return "", err
	} else {
		response = response[0 : n-2] // chop \n\r
	}

	return string(response), nil
}

func (c connection) eventZone(zone int, event event, parameter string) (string, error) {
	cmd := fmt.Sprintf("event c[1].z[%d]!%s", zone, event)
	if parameter != "" {
		cmd += " " + parameter
	}
	c.log.Printf("running '%s'\n", cmd)
	cmd += "\n\r"

	if response, err := c.run(cmd); err != nil {
		return "", err
	} else if response != "S" {
		return "", fmt.Errorf("unexpected response: %+v", []byte(response))
	}

	return "", nil
}

func (c connection) allOff() error {
	_, err := c.eventZone(1, EventAllOff, "")
	return err
}

func (c connection) connect() (net.Conn, error) {
	dialer := net.Dialer{}
	if conn, err := dialer.Dial("tcp", c.remoteIp+":9621"); err != nil {
		return nil, err
	} else {
		c.log.Printf("connected")
		return conn, nil
	}
}

type controller struct {
	c   connection
	log *log.Logger
}

func loadZones(r *http.Request) ([]int, error) {
	var speaker speakerEvent

	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&speaker)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling body: %s", err)
	} else if speaker.Speaker == "" {
		return nil, fmt.Errorf("missing speaker name", err)
	} else if zones := zoneLookup(speaker.Speaker); zones == nil {
		return nil, fmt.Errorf("unknown speaker name: %s", speaker.Speaker)
	} else {
		return zones, nil
	}
}

func (controller controller) speakerOn(w http.ResponseWriter, r *http.Request) {
	zones, err := loadZones(r)
	if err != nil {
		controller.log.Printf("%s", err)
	}

	for _, zone := range zones {
		if _, err = controller.c.eventZone(zone, EventSelectSource, SourceAux); err != nil {
			controller.log.Printf("error turning on zone %d: %s", zone, err)
		} else {
			log.Printf("turned on zone %d", zone)
		}
	}
}

func (controller controller) speakerVolumeUp(w http.ResponseWriter, r *http.Request) {
	zones, err := loadZones(r)
	if err != nil {
		controller.log.Printf("%s", err)
	}

	for _, zone := range zones {
		if _, err = controller.c.eventZone(zone, EventKeyPress, KeyPressVolumeUp); err != nil {
			controller.log.Printf("error turning on zone %d: %s", zone, err)
		} else {
			log.Printf("volume up on zone %d", zone)
		}
	}
}

func (controller controller) speakerVolumeDown(w http.ResponseWriter, r *http.Request) {
	zones, err := loadZones(r)
	if err != nil {
		controller.log.Printf("%s", err)
	}

	for _, zone := range zones {
		if _, err = controller.c.eventZone(zone, EventKeyPress, KeyPressVolumeDown); err != nil {
			controller.log.Printf("error turning on zone %d: %s", zone, err)
		} else {
			log.Printf("volume down on zone %d", zone)
		}
	}
}

func (controller controller) speakerOff(w http.ResponseWriter, r *http.Request) {
	zones, err := loadZones(r)
	if err != nil {
		controller.log.Printf("%s", err)
	}

	for _, zone := range zones {
		if _, err = controller.c.eventZone(zone, EventZoneOff, ""); err != nil {
			controller.log.Printf("error turning off zone %d: %s", zone, err)
		} else {
			log.Printf("turned off zone %d", zone)
		}
	}
}

var magic = "////////xmENdY3mCABFAACCgcVAAEARo5IKAAAVCgAA/yWVJZUAbnZB////////ACHHAGtnACHH" +
	"AGtnACHHAGtnACHHAGtnACHHAGtnACHHAGtnACHHAGtnACHHAGtnACHHAGtnACHHAGtnACHHAGtn" +
	"ACHHAGtnACHHAGtnACHHAGtnACHHAGtnACHHAGtn"

func wol() error {
	laddr, err := net.ResolveUDPAddr("udp4", ":9621")
	if err != nil {
		return err
	}

	raddr, err := net.ResolveUDPAddr("udp4", "10.0.0.255:9621")
	if err != nil {
		return err
	}

	magicBytes, err := base64.StdEncoding.DecodeString(magic)
	if err != nil {
		return err
	}

	fmt.Printf("%d %T\n", len(magicBytes), magicBytes)

	udp, err := net.DialUDP("udp4", laddr, raddr)
	if err != nil {
		return err
	}

	defer udp.Close()

	n, err := udp.Write(magicBytes)
	if err != nil {
		return err
	} else if len(magicBytes) != n {
		return errors.New("short udp write")
	}

	return nil
}

func main() {
	l := log.New(os.Stderr, "", log.Ldate|log.Ltime)

	controller := controller{
		c: connection{
			remoteIp: "10.0.0.131",
			log:      l,
		},
		log: l,
	}

	http.HandleFunc("/ifttt/speaker/on", controller.speakerOn)
	http.HandleFunc("/ifttt/speaker/off", controller.speakerOff)
	http.HandleFunc("/ifttt/speaker/volume/up", controller.speakerVolumeUp)
	http.HandleFunc("/ifttt/speaker/volume/down", controller.speakerVolumeDown)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
		log.Printf("Hello, %q", html.EscapeString(r.URL.Path))
	})

	log.Printf("serving")

	log.Fatal(http.ListenAndServe(":8080", nil))

	//if err = conn.allOff(); err != nil {
	//log.Fatalf("run: %s", err)
	//}
}
