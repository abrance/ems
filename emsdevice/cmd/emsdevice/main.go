package main

import (
	"context"
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/abrance/ems/emsdevice/internal/config"
	"github.com/abrance/ems/emsdevice/internal/essdemo"
	"github.com/abrance/ems/emsdevice/internal/mock"
	"github.com/abrance/ems/emsdevice/pkg/modbus/slave"
)

func main() {
	configPath := flag.String("config", os.Getenv("EMSDEVICE_CONFIG"), "JSON config path")
	listen := flag.String("listen", "", "Modbus TCP listen URL")
	unitID := flag.Int("unit-id", -1, "Modbus unit id served to OpenLEMS")
	mockMode := flag.String("mock", "", "mock mode: static or live")
	mockInterval := flag.String("mock-interval", "", "live mock tick interval")
	mockSOC := flag.Float64("mock-soc", math.NaN(), "mock SOC percent")
	mockPower := flag.Float64("mock-power", math.NaN(), "mock power kW")
	flag.Parse()

	ov := config.Overrides{
		Listen:   *listen,
		Mode:     *mockMode,
		Interval: *mockInterval,
	}
	if *unitID >= 0 {
		n := uint(*unitID)
		ov.UnitID = &n
	}
	if !math.IsNaN(*mockSOC) {
		ov.SOC = mockSOC
	}
	if !math.IsNaN(*mockPower) {
		ov.Power = mockPower
	}

	cfg, err := config.Load(*configPath, ov)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	handler := slave.NewHandler(uint8(cfg.UnitID), cfg.Mock.Values.Registers())
	server, err := slave.NewServer(cfg.Listen, handler)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}
	if err := server.Start(); err != nil {
		log.Fatalf("start server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.Mock.Mode == config.ModeLive {
		go mock.Run(ctx, handler, mock.Settings{
			Interval:    cfg.Mock.Interval,
			SOCJitter:   cfg.Mock.SOCJitter,
			PowerJitter: cfg.Mock.PowerJitter,
		})
	}
	log.Printf("emsdevice listening %s unitId=%d mock=%s map=0x%X-0x%X", cfg.Listen, cfg.UnitID, cfg.Mock.Mode, essdemo.BaseAddr, essdemo.BaseAddr+essdemo.Count-1)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()
	if err := server.Stop(); err != nil {
		log.Printf("stop server: %v", err)
	}
}
