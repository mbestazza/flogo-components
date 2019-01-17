package mqtt

import (
	"context"

	"crypto/tls"
	"crypto/x509"
	// "encoding/json"
	"io/ioutil"
	"strconv"


	//"github.com/TIBCOSoftware/flogo-contrib/action/flow/support"


	"github.com/TIBCOSoftware/flogo-lib/core/action"
	"github.com/TIBCOSoftware/flogo-lib/core/trigger"

	"github.com/TIBCOSoftware/flogo-lib/logger"
	"github.com/eclipse/paho.mqtt.golang"
	//"time"
	
)

// log is the default package logger
var log = logger.GetLogger("trigger-tibco-mqtt")


// todo: switch to use endpoint registration

// MqttTrigger is simple MQTT trigger
type MqttTrigger struct {
	metadata        *trigger.Metadata
	runner          action.Runner
	client          mqtt.Client
	config          *trigger.Config
	topicToActionId map[string]string
}

//NewFactory create a new Trigger factory
func NewFactory(md *trigger.Metadata) trigger.Factory {
	return &MQTTFactory{metadata: md}
}

// MQTTFactory Trigger factory
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

	opts := mqtt.NewClientOptions()
	opts.AddBroker(t.config.GetSetting("broker"))
	opts.SetClientID(t.config.GetSetting("id"))
	opts.SetUsername(t.config.GetSetting("user"))
	opts.SetPassword(t.config.GetSetting("password"))
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
		log.Infof("Received msg: %s", payload)
		log.Infof("Actual topic: %s", topic)

		// try topic without wildcards
		actionId, found := t.topicToActionId[topic]

		if found {
			t.RunAction(actionId, payload, topic)
		} else {
			// search for wildcards

			for _, handlerCfg := range t.config.Handlers {
				eptopic := handlerCfg.GetSetting("topic")
				if strings.HasSuffix(eptopic, "/#") {
					// is wildcard, now check actual topic starts with wildcard
					if strings.HasPrefix(topic, strings.TrimSuffix(eptopic, "/#")) {
						// Got a match, now get the action for the wildcard topic
						//actionType, found := t.topicToActionType[eptopic]
						actionId, found := t.topicToActionId[eptopic]
						if found {
							t.RunAction(actionId, payload, topic)
						}
					}
				}
			}
		}

	})

	//set tls config
	tlsConfig := NewTLSConfig("")
	opts.SetTLSConfig(tlsConfig)
	
	client := mqtt.NewClient(opts)
	t.client = client
	log.Infof("Connecting to broker [%s]", t.config.GetSetting("broker"))
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	log.Info("Connected to broker")

	i, err := strconv.Atoi(t.config.GetSetting("qos"))
	if err != nil {
		log.Error("Error converting \"qos\" to an integer ", err.Error())
		return err
	}

	//	t.topicToActionType = make(map[string]string)
	t.topicToActionId = make(map[string]string)

	for _, handlerCfg := range t.config.Handlers {
		if token := t.client.Subscribe(handlerCfg.GetSetting("topic"), byte(i), nil); token.Wait() && token.Error() != nil {
			log.Errorf("Error subscribing to topic %s: %s", handlerCfg.Settings["topic"], token.Error())
			panic(token.Error())
		} else {
			log.Infof("Subscribed to topic %s for action %s", handlerCfg.GetSetting("topic"), handlerCfg.ActionId)
			t.topicToActionId[handlerCfg.GetSetting("topic")] = handlerCfg.ActionId
		}
	}
	/*
		//stay here
		for {
		} */
	return nil
}

// Stop implements ext.Trigger.Stop
func (t *MqttTrigger) Stop() error {
	//unsubscribe from topics
	for _, handlerCfg := range t.config.Handlers {
		log.Infof("Unsubcribing from topic: %s ", handlerCfg.GetSetting("topic"))
		if token := t.client.Unsubscribe(handlerCfg.GetSetting("topic")); token.Wait() && token.Error() != nil {
			log.Errorf("Error unsubscribing from topic %s: %s", handlerCfg.GetSetting("topic"), token.Error())
		}
	}

	t.client.Disconnect(250)

	return nil
}

// RunAction starts a new Process Instance
func (t *MqttTrigger) RunAction(actionId string, payload string, topic string) {

		log.Info("Starting new Process Instance")
		log.Infof("Action Id: %s", actionId)
		log.Infof("Payload: %s", payload)
		log.Infof("Actual Topic: %s", topic) 

	req := t.constructStartRequest(payload, topic)
	//err := json.NewDecoder(strings.NewReader(payload)).Decode(req)
	//if err != nil {
	//	//http.Error(w, err.Error(), http.StatusBadRequest)
	//	log.Error("Error Starting action ", err.Error())
	//	return
	//}

	//todo handle error
	//	log.Infof("Got data: %s", req.Data)
	startAttrs, _ := t.metadata.OutputsToAttrs(req.Data, false)
	action := action.Get(actionId)
	context := trigger.NewContext(context.Background(), startAttrs)

	//todo handle error
	_, replyData, err := t.runner.Run(context, action, actionId, nil)
	if err != nil {
		log.Error(err)
	}

	log.Debugf("Ran action: [%s]", actionId)
	log.Debugf("Reply data: [%s]", replyData)

	/*
		if replyData != nil {
			data, err := json.Marshal(replyData)
			if err != nil {
				log.Error(err)
			} else {
				t.publishMessage(req.ReplyTo, string(data))
			}
		} */
}

func (t *MqttTrigger) publishMessage(topic string, message string) {

	log.Debug("ReplyTo topic: ", topic)
	log.Debug("Publishing message: ", message)

	qos, err := strconv.Atoi(t.config.GetSetting("qos"))
	if err != nil {
		log.Error("Error converting \"qos\" to an integer ", err.Error())
		return
	}
	token := t.client.Publish(topic, byte(qos), false, message)
	token.Wait()
}

func (t *MqttTrigger) constructStartRequest(message string, topic string) *StartRequest {

	log.Debug("Received contstruct start request")

	//TODO how to handle reply to, reply feature
	req := &StartRequest{}
	data := make(map[string]interface{})
	data["message"] = message
	data["actualtopic"] = topic
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
	ReplyTo     string                 `json:"replyTo"`
}
