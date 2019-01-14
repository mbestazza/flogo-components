package mqtt

import (
	"context"

	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io/ioutil"
	"strconv"
	
	"github.com/TIBCOSoftware/flogo-contrib/action/flow/support"

	"github.com/TIBCOSoftware/flogo-lib/logger"
	"time"
	
	"fmt"
	"github.com/TIBCOSoftware/flogo-lib/core/activity"

	"github.com/eclipse/paho.mqtt.golang"
)

// log is the default package logger
var log = logger.GetLogger("activity-mbestazza-mqtt")

const (
	broker   = "broker"
	topic    = "topic"
	qos      = "qos"
	payload  = "message"
	id       = "id"
	user     = "user"
	password = "password"
)

// MyActivity is a stub for your Activity implementation
type MyActivity struct {
	metadata *activity.Metadata
}

// NewActivity creates a new AppActivity
func NewActivity(metadata *activity.Metadata) activity.Activity {
	return &MyActivity{metadata: metadata}
}

// Metadata implements activity.Activity.Metadata
func (a *MyActivity) Metadata() *activity.Metadata {
	return a.metadata
}

// Eval implements activity.Activity.Eval
func (a *MyActivity) Eval(context activity.Context) (done bool, err error) {

	// do eval

	brokerInput := context.GetInput(broker)

	ivbroker, ok := brokerInput.(string)
	if !ok {
		context.SetOutput("result", "BROKER_NOT_SET")
		return true, fmt.Errorf("broker not set")
	}

	topicInput := context.GetInput(topic)

	ivtopic, ok := topicInput.(string)
	if !ok {
		context.SetOutput("result", "TOPIC_NOT_SET")
		return true, fmt.Errorf("topic not set")
	}

	payloadInput := context.GetInput(payload)

	ivpayload, ok := payloadInput.(string)
	if !ok {
		context.SetOutput("result", "PAYLOAD_NOT_SET")
		return true, fmt.Errorf("payload not set")
	}

	ivqos, ok := context.GetInput(qos).(int)

	if !ok {
		context.SetOutput("result", "QOS_NOT_SET")
		return true, fmt.Errorf("qos not set")
	}

	idInput := context.GetInput(id)

	ivID, ok := idInput.(string)
	if !ok {
		context.SetOutput("result", "CLIENTID_NOT_SET")
		return true, fmt.Errorf("client id not set")
	}

	userInput := context.GetInput(user)

	ivUser, ok := userInput.(string)
	if !ok {
		//User not set, use default
		ivUser = ""
	}

	passwordInput := context.GetInput(password)

	ivPassword, ok := passwordInput.(string)
	if !ok {
		//Password not set, use default
		ivPassword = ""
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(ivbroker)
	opts.SetClientID(ivID)
	opts.SetUsername(ivUser)
	opts.SetPassword(ivPassword)
	
	//set tls config
	tlsConfig := NewTLSConfig("")
	opts.SetTLSConfig(tlsConfig)
	client := mqtt.NewClient(opts)

	log.Debugf("MQTT Publisher connecting")
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	log.Debugf("MQTT Publisher connected, sending message")
	token := client.Publish(ivtopic, byte(ivqos), false, ivpayload)
	token.Wait()

	client.Disconnect(250)
	log.Debugf("MQTT Publisher disconnected")
	context.SetOutput("result", "OK")

	return true, nil
}

// NewTLSConfig creates a TLS configuration for the specified 'thing'
func NewTLSConfig(thingName string) *tls.Config {
	// Import root CA
	certpool := x509.NewCertPool()
	pemCerts, err := ioutil.ReadFile("root-CA.pem")
	if err == nil {
		certpool.AppendCertsFromPEM(pemCerts)
	}

	//thingDir := "things/" + thingName + "/"

	// Import client certificate/key pair for the specified 'thing'
	cert, err := tls.LoadX509KeyPair("device.pem.crt", "device.pem.key")
	if err != nil {
		panic(err)
	}

	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		panic(err)
	}

	return &tls.Config{
		RootCAs:            certpool,
		ClientAuth:         tls.NoClientCert,
		ClientCAs:          nil,
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{cert},
	}
}
