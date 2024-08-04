package main

/*
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/common/pico_stdlib/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/common/pico_base/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2_common/pico_platform/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2040/hardware_regs/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2040/hardware_structs/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2_common/pico_stdio/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2_common/hardware_timer/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2_common/hardware_gpio/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2_common/hardware_sync/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2_common/hardware_uart/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2_common/hardware_irq/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/rp2_common/hardware_base/include
#cgo CFLAGS: -I/data/data/com.termux/files/home/pico/pico-sdk/src/common/pico_time/include

#include "pico/stdlib.h"
// bool __no_inline_not_in_flash_func(get_bootsel_button)();
__attribute__((noinline,section(".data"))) bool get_bootsel_button();
// bool get_bootsel_button();
*/
import "C"

import (
	"time"

	"github.com/soypat/cyw43439"
)

func main() {
	// Wait for USB to initialize:
	time.Sleep(time.Second)
	dev := cyw43439.NewPicoWDevice()
	cfg := cyw43439.DefaultWifiConfig()
	// cfg.Logger = logger // Uncomment to see in depth info on wifi device functioning.
	err := dev.Init(cfg)
	if err != nil {
		panic(err)
	}
	bl := 500 * time.Millisecond
	for {
		if C.get_bootsel_button() {
			bl = 100 * time.Millisecond
		} else {
			bl = 500 * time.Millisecond
		}
		err = dev.GPIOSet(0, true)
		if err != nil {
			println("err", err.Error())
		} else {
			println("LED ON")
		}
		time.Sleep(bl)
		err = dev.GPIOSet(0, false)
		if err != nil {
			println("err", err.Error())
		} else {
			println("LED OFF")
		}
		time.Sleep(bl)
	}
}
