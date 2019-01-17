package mqtt

import (
	"context"

	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io/ioutil"
	"strconv"


	"github.com/TIBCOSoftware/flogo-contrib/action/flow/support"


	"github.com/TIBCOSoftware/flogo-lib/core/action"
	"github.com/TIBCOSoftware/flogo-lib/core/trigger"

	"github.com/TIBCOSoftware/flogo-lib/logger"
	"github.com/eclipse/paho.mqtt.golang"
	"time"
	
)

// log is the default package logger
var log = logger.GetLogger("trigger-tibco-mqtt")

// MqttTrigger is simple MQTT trigger
type MqttTrigger struct {
	metadata         *trigger.Metadata
	runner           action.Runner
	client           mqtt.Client
	config           *trigger.Config
	topicToActionURI map[string]string
}

//NewFactory create a new Trigger factory
func NewFactory(md *trigger.Metadata) trigger.Factory {
	return &MQTTFactory{metadata: md}
}

// MQTTFactory MQTT Trigger factory
type MQTTFactory struct {
	metadata *trigger.Metadata
}

//New Creates a new trigger instance for a given id
func (t *MQTTFactory) New(config *trigger.Config) trigger.Trigger {
	return &MqttTrigger{metadata: t.metadata, config: config}
}

// Metadata implements trigger.Trigger.Metadata
func (t *MqttTrigger) Metadata() *trigger.Metadata {
	return t.metadata
}

// Init implements ext.Trigger.Init
func (t *MqttTrigger) Init(runner action.Runner) {
	t.runner = runner
}

// Start implements ext.Trigger.Start
func (t *MqttTrigger) Start() error {

	idInput := t.config.GetSetting("id")

	if idInput == "" {
		log.Error("Error client id not set")
		idInput = "flogo"
	}
	
	userInput := t.config.GetSetting("user")
	if userInput == "" {
		log.Error("Error userInput not set")
		userInput = ""
	}
	

	passwordInput := t.config.GetSetting("password")
	if passwordInput == "" {
		log.Error("Error passwordInput  not set")
		passwordInput = ""
	}
	
	
	opts := mqtt.NewClientOptions()
	opts.AddBroker(t.config.GetSetting("broker"))
	opts.SetClientID(idInput)
	opts.SetUsername(userInput)
	opts.SetPassword(passwordInput)

log.Error("after opts.SetPassword")



	b, err := strconv.ParseBool(t.config.GetSetting("cleansess"))
	if err != nil {
		log.Error("Error converting \"cleansess\" to a boolean ", err.Error())
		return err
	}
	opts.SetCleanSession(b)
	if storeType := t.config.Settings["store"]; storeType != ":memory:" {
		opts.SetStore(mqtt.NewFileStore(t.config.GetSetting("store")))
	}

	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		topic := msg.Topic()
		//TODO we should handle other types, since mqtt message format are data-agnostic
		payload := string(msg.Payload())
		log.Debug("Received msg:", payload)
		actionURI, found := t.topicToActionURI[topic]
		if found {
			t.RunAction(actionURI, payload)
		} else {
			log.Errorf("Topic %s not found", t.topicToActionURI[topic])
		}
	})
	
log.Error("after opts.SetDefaultPublishHandler")

	//set tls config
	tlsConfig := NewTLSConfig("")
	opts.SetTLSConfig(tlsConfig)
	
log.Error("Before Client")
	
	client := mqtt.NewClient(opts)
	t.client = client
	log.Infof("Connecting to broker [%s]", t.config.GetSetting("broker"))
	
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	i, err := strconv.Atoi(t.config.GetSetting("qos"))
	if err != nil {
		log.Error("Error converting \"qos\" to an integer ", err.Error())
		return err
	}

	t.topicToActionURI = make(map[string]string)
log.Error("after t.topicToActionURI")
	
	for _, handlerCfg := range t.config.Handlers {

		topic := handlerCfg.GetSetting("topic")

		if token := t.client.Subscribe(topic, byte(i), nil); token.Wait() && token.Error() != nil {
			log.Errorf("Error subscribing to topic %s: %s", topic, token.Error())
			panic(token.Error())
		} else {
			log.Debugf("Suscribed to topic: %s, will trigger actionId: %s", topic, handlerCfg.ActionId)
			t.topicToActionURI[topic] = handlerCfg.ActionId
		}
	}

	return nil
}

// Stop implements ext.Trigger.Stop
func (t *MqttTrigger) Stop() error {
	//unsubscribe from topic
	log.Debug("Unsubcribing from topic: ", t.config.Settings["topic"])
	for _, handlerCfg := range t.config.Handlers {
		if token := t.client.Unsubscribe(handlerCfg.GetSetting("topic")); token.Wait() && token.Error() != nil {
			log.Errorf("Error unsubscribing from topic %s: %s", handlerCfg.Settings["topic"], token.Error())
		}
	}

	t.client.Disconnect(250)

	return nil
}

// RunAction starts a new Process Instance
func (t *MqttTrigger) RunAction(actionURI string, payload string) {

	req := t.constructStartRequest(payload)
	//err := json.NewDecoder(strings.NewReader(payload)).Decode(req)
	//if err != nil {
	//	//http.Error(w, err.Error(), http.StatusBadRequest)
	//	log.Error("Error Starting action ", err.Error())
	//	return
	//}

	//todo handle error
	startAttrs, _ := t.metadata.OutputsToAttrs(req.Data, false)

	action := action.Get(actionURI)
	context := trigger.NewContext(context.Background(), startAttrs)
	_, replyData, err := t.runner.Run(context, action, actionURI, nil)
	if err != nil {
		log.Error("Error starting action: ", err.Error())
	}
	log.Debugf("Ran action: [%s]", actionURI)

	if replyData != nil {
		data, err := json.Marshal(replyData)
		if err != nil {
			log.Error(err)
		} else {
			t.publishMessage(req.ReplyTo, string(data))
		}
	}
}

func (t *MqttTrigger) publishMessage(topic string, message string) {

	log.Debug("ReplyTo topic: ", topic)
	log.Debug("Publishing message: ", message)

	qos, err := strconv.Atoi(t.config.GetSetting("qos"))
	if err != nil {
		log.Error("Error converting \"qos\" to an integer ", err.Error())
		return
	}
	if len(topic) == 0 {
		log.Warn("Invalid empty topic to publish to")
		return
	}
	token := t.client.Publish(topic, byte(qos), false, message)

	sent := token.WaitTimeout(5000 * time.Millisecond)
	if !sent {
		// Timeout occurred
		log.Errorf("Timeout occurred while trying to publish to topic '%s'", topic)
		return
	}
}

func (t *MqttTrigger) constructStartRequest(message string) *StartRequest {
	//TODO how to handle reply to, reply feature
	req := &StartRequest{}
	data := make(map[string]interface{})
	data["message"] = message
	req.Data = data
	return req
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

// StartRequest describes a request for starting a ProcessInstance
type StartRequest struct {
	ProcessURI  string                 `json:"flowUri"`
	Data        map[string]interface{} `json:"data"`
	Interceptor *support.Interceptor   `json:"interceptor"`
	Patch       *support.Patch         `json:"patch"`
	ReplyTo     string                 `json:"replyTo"`
}
