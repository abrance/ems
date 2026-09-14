package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/abrance/ems/emsdevice/internal/essdemo"
	"github.com/abrance/ems/emsdevice/pkg/modbus/slave"
)

func main() {
	listen := flag.String("listen", "tcp://0.0.0.0:1502", "Modbus TCP listen URL")
	unitID := flag.Uint("unit-id", 1, "Modbus unit id served to OpenLEMS")
	flag.Parse()

	handler := slave.NewHandler(uint8(*unitID), essdemo.DefaultRegisters())
	server, err := slave.NewServer(*listen, handler)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}
	if err := server.Start(); err != nil {
		log.Fatalf("start server: %v", err)
	}
	log.Printf("emsdevice listening %s unitId=%d map=0x%X-0x%X", *listen, *unitID, essdemo.BaseAddr, essdemo.BaseAddr+essdemo.Count-1)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	if err := server.Stop(); err != nil {
		log.Printf("stop server: %v", err)
	}
}
