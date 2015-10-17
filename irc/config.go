package irc

import (
	"code.google.com/p/gcfg"
	"errors"
	"log"
	"crypto/tls"
)

type PassConfig struct {
	Password string
}

// SSLListenConfig defines configuration options for listening on SSL
type SSLListenConfig struct {
	SSLCert string
	SSLKey  string
}

// Certificate returns the SSL certificate assicated with this SSLListenConfig
func (conf *SSLListenConfig) Config() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(conf.SSLCert, conf.SSLKey)
	if err != nil {
		log.Fatal("sslconf+sslkey: invalid pair. Error: ", err)
		return nil, errors.New("sslconf+sslkey: invalid pair")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}, err
}

func (conf *PassConfig) PasswordBytes() []byte {
	bytes, err := DecodePassword(conf.Password)
	if err != nil {
		log.Fatal("decode password error: ", err)
	}
	return bytes
}

type Config struct {
	Server struct {
		PassConfig
		Database string
		Listen   []string
		Wslisten string
		Log      string
		MOTD     string
		Name     string
	}

	Operator map[string]*PassConfig

	Theater map[string]*PassConfig

	SSLListener map[string]*SSLListenConfig
}

func (conf *Config) Operators() map[Name][]byte {
	operators := make(map[Name][]byte)
	for name, opConf := range conf.Operator {
		operators[NewName(name)] = opConf.PasswordBytes()
	}
	return operators
}

func (conf *Config) Theaters() map[Name][]byte {
	theaters := make(map[Name][]byte)
	for s, theaterConf := range conf.Theater {
		name := NewName(s)
		if !name.IsChannel() {
			log.Fatal("config uses a non-channel for a theater!")
		}
		theaters[name] = theaterConf.PasswordBytes()
	}
	return theaters
}

func (conf *Config) SSLListeners() map[Name]*tls.Config {
	sslListeners := make(map[Name]*tls.Config)
	for s, sslListenersConf := range conf.SSLListener {
		config, err := sslListenersConf.Config()
		if err != nil {
			log.Fatal(err)
		}
		sslListeners[NewName(s)] = config
	}
	return sslListeners
}

func LoadConfig(filename string) (config *Config, err error) {
	config = &Config{}
	err = gcfg.ReadFileInto(config, filename)
	if err != nil {
		return
	}
	if config.Server.Name == "" {
		err = errors.New("server.name missing")
		return
	}
	if config.Server.Database == "" {
		err = errors.New("server.database missing")
		return
	}
	if len(config.Server.Listen) == 0 {
		err = errors.New("server.listen missing")
		return
	}
	return
}
