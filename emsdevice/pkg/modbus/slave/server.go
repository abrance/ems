package slave

import (
	"log"
	"time"

	"github.com/simonvetter/modbus"
)

type Server struct {
	inner *modbus.ModbusServer
}

func NewServer(listen string, handler *Handler) (*Server, error) {
	inner, err := modbus.NewServer(&modbus.ServerConfiguration{
		URL:        listen,
		Timeout:    30 * time.Second,
		MaxClients: 8,
		Logger:     log.Default(),
	}, handler)
	if err != nil {
		return nil, err
	}
	return &Server{inner: inner}, nil
}

func (s *Server) Start() error {
	return s.inner.Start()
}

func (s *Server) Stop() error {
	return s.inner.Stop()
}
