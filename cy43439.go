/*
# Notes on Endianness.

Endianness is the order or sequence of bytes of a word of digital data in computer memory.

  - A big-endian system stores the most significant byte of a word at the
    smallest memory address and the least significant byte at the largest.
  - A little-endian system, in contrast, stores the least-significant byte
    at the smallest address.

Endianness may also be used to describe the order in which the bits are
transmitted over a communication channel

  - big-endian in a communications channel transmits the most significant bits first
*/
package cyw43439

import (
	"encoding/binary"
	"errors"
	"machine"
	"time"
	"unsafe"

	"tinygo.org/x/drivers"
)

func PicoWSpi() (spi drivers.SPI, cs, wlRegOn, irq machine.Pin) {
	// Raspberry Pi Pico W pin definitions for the CY43439.
	const (
		WL_REG_ON = machine.GPIO23
		DATA_OUT  = machine.GPIO24
		DATA_IN   = machine.GPIO24
		IRQ       = machine.GPIO24
		CLK       = machine.GPIO29
		CS        = machine.GPIO25
	)
	// Need software spi implementation since Rx/Tx are on same pin.
	CS.Configure(machine.PinConfig{Mode: machine.PinOutput})
	CLK.Configure(machine.PinConfig{Mode: machine.PinOutput})
	// DATA_IN.Configure(machine.PinConfig{Mode: machine.PinInput})
	DATA_OUT.Configure(machine.PinConfig{Mode: machine.PinOutput})
	spi = &SPIbb{
		SCK:   CLK,
		SDI:   DATA_IN,
		SDO:   DATA_OUT,
		Delay: 1 << 10,
	}
	return spi, CS, WL_REG_ON, IRQ
}

type Dev struct {
	spi drivers.SPI
	// Chip select pin. Driven LOW during SPI transaction.
	lastSize               uint32
	lastHeader             [2]uint32
	currentBackplaneWindow uint32
	lastBackplaneWindow    uint32
	cs                     machine.Pin
	wlRegOn                machine.Pin
	irq                    machine.Pin
	sharedSD               machine.Pin
	buf                    [2000 / 4]uint32
}

type Config struct {
}

func NewDev(spi drivers.SPI, cs, wlRegOn, irq, sharedSD machine.Pin) *Dev {
	SD := machine.NoPin
	if sharedDATA && sharedSD != machine.NoPin {
		SD = sharedSD // Pico W special case.
	}
	return &Dev{
		spi:      spi,
		cs:       cs,
		wlRegOn:  wlRegOn,
		sharedSD: SD,
	}
}

func (d *Dev) Init() (err error) {
	/*
		To initiate communication through the gSPI after power-up, the host
		needs to bring up the WLAN chip by writing to the wake-up WLAN
		register bit. Writing a 1 to this bit will start up the necessary
		crystals and PLLs so that the CYW43439 is ready for data transfer. The
		device can signal an interrupt to the host indicating that the device
		is awake and ready. This procedure also needs to be followed for
		waking up the device in sleep mode. The device can interrupt the host
		using the WLAN IRQ line whenever it has any information to
		pass to the host. On getting an interrupt, the host needs to read the
		interrupt and/or status register to determine the cause of the
		interrupt and then take necessary actions.
	*/
	d.gpioSetup()
	d.wlRegOn.High() //
	// After power-up, the gSPI host needs to wait 50 ms for the device to be out of reset.
	time.Sleep(60 * time.Millisecond) // it's actually slightly more than 50ms, including VDDC and POR startup.
	// For this, the host needs to poll with a read command
	// to F0 address 0x14. Address 0x14 contains a predefined bit pattern.
	startPoll := time.Now()
	var got uint32
	// Little endian test address values.
	const (
		pollAddr   = 0x1400
		pollExpect = 0xADBEEDFE // Little endian 0xFEEDBEAD
	)
	for got != pollExpect {
		got, err = d.RegisterReadUint32(FuncAllSPIRegisters, pollAddr)
		if err != nil {
			return err
		}
		if got != pollExpect && time.Since(startPoll) > pollLimit {
			print("poll failed with ")
			println(got)
			return errors.New("poll failed")
		}
	}
	// Address 0x0000 registers (little-endian).
	const (
		WordLengthPos   = 31
		EndianessPos    = 30
		HiSpeedModePos  = 27
		InterruptPolPos = 26
		WakeUpPos       = 24
	)
	// Write wake-up bit, switch to 32 bit SPI, and keep default interrupt polarity.
	err = d.RegisterWriteUint32(FuncAllSPIRegisters, 0x0, (1<<WakeUpPos)|
		(1<<InterruptPolPos)|(0<<HiSpeedModePos)|(0<<EndianessPos)|(1<<WordLengthPos))
	if err != nil {
		return err
	}
	return nil
}

type Function uint32

const (
	// All SPI-specific registers.
	FuncAllSPIRegisters Function = 0b00
	// Registers and memories belonging to other blocks in the chip (64 bytes max).
	FuncExtraSPIRegisters Function = 0b01
	// DMA channel 1. WLAN packets up to 2048 bytes.
	FuncDMA1 Function = 0b10
	// DMA channel 2 (optional). Packets up to 2048 bytes.
	FuncDMA2 Function = 0b11
)

const (
	TestRegisterAddr              uint32        = 0x14
	TestRegisterExpectedValue     uint32        = 0xFEEDBEAD
	responseDelay                 time.Duration = 0 //20 * time.Microsecond
	backplaneFunction                           = 0
	whdBusSPIBackplaneReadPadding               = 4
	sharedDATA                                  = true
	pollLimit                                   = 100 * time.Millisecond
)

func (d *Dev) RegisterReadUint32(fn Function, reg uint32) (uint32, error) {
	val, err := d.readReg(fn, reg, 4)
	return uint32(val), err
}

func (d *Dev) RegisterReadUint16(fn Function, reg uint32) (uint16, error) {
	val, err := d.readReg(fn, reg, 2)
	return uint16(val), err
}

func (d *Dev) RegisterReadUint8(fn Function, reg uint32) (uint8, error) {
	val, err := d.readReg(fn, reg, 1)
	return uint8(val), err
}

func (d *Dev) readReg(fn Function, reg uint32, size int) (uint32, error) {
	var padding uint32
	if fn == backplaneFunction {
		padding = whdBusSPIBackplaneReadPadding
	}
	cmd := make_cmd(false, true, fn, reg, uint32(size)+padding)
	var buf [4 + whdBusSPIBackplaneReadPadding]byte
	err := d.SPIRead(cmd, buf[:4+padding])
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:4]), nil
}

func (d *Dev) RegisterWriteUint32(fn Function, reg, val uint32) error {
	return d.writeReg(fn, reg, val, 4)
}

func (d *Dev) RegisterWriteUint16(fn Function, reg uint32, val uint16) error {
	return d.writeReg(fn, reg, uint32(val), 2)
}

func (d *Dev) RegisterWriteUint8(fn Function, reg uint32, val uint8) error {
	return d.writeReg(fn, reg, uint32(val), 1)
}

func (d *Dev) writeReg(fn Function, reg, val, size uint32) error {
	var buf [4]byte
	cmd := make_cmd(true, true, fn, reg, size)
	binary.BigEndian.PutUint32(buf[:], val)
	if fn == backplaneFunction {
		d.lastSize = 8
		d.lastHeader[0] = cmd
		d.lastHeader[1] = val
		d.lastBackplaneWindow = d.currentBackplaneWindow
	}
	return d.SPIWrite(cmd, buf[:size])
}

func (d *Dev) SPIWriteRead(command uint32, r []byte) error {
	d.cs.Low()
	err := d.spiWrite32(command, nil)
	if err != nil {
		return err
	}
	if sharedDATA {
		d.sharedSD.Configure(machine.PinConfig{Mode: machine.PinInputPulldown})
	}
	d.responseDelay()
	err = d.spi.Tx(nil, r)
	d.cs.High()
	return err
}

func (d *Dev) SPIRead(command uint32, r []byte) error {
	// if len(r)%4 != 0 {
	// 	return errors.New("read buffer length must be multiple of 4")
	// }

	d.cs.Low()
	err := d.spiWrite32(command, nil)
	d.cs.High()
	if err != nil {
		return err
	}
	if sharedDATA {
		d.sharedSD.Configure(machine.PinConfig{Mode: machine.PinInputPulldown})
	}
	d.cs.Low()
	d.responseDelay()
	// n := len(r) / 4
	// for i := 0; i < n; i++ {
	// 	offset := i * 4
	// 	r[offset], _ = d.spi.Transfer(0)
	// 	r[offset+1], _ = d.spi.Transfer(0)
	// 	r[offset+2], _ = d.spi.Transfer(0)
	// 	r[offset+3], _ = d.spi.Transfer(0)
	// }
	err = d.spi.Tx(nil, r)
	d.cs.High()
	return err
}

// SPIWrite interprets w as a slice of 32bit words
func (d *Dev) SPIWrite(command uint32, w []byte) error {
	d.cs.Low()
	err := d.spiWrite32(command, unsafe.Slice((*uint32)(unsafe.Pointer(&w[0])), len(w)/4))
	d.cs.High()
	return err
}

// SPIWrite interprets w as a slice of 32bit words
func (d *Dev) SPIWrite32(command uint32, w []uint32) error {
	if len(w)%4 != 0 {
		return errors.New("length of buffer must be multiple of 4")
	}
	d.cs.Low()
	err := d.spiWrite32(command, unsafe.Slice((*uint32)(unsafe.Pointer(&w[0])), len(w)/4))
	d.cs.High()
	return err
}

func (d *Dev) spiWrite16(command uint32, w []uint16) error {
	if sharedDATA {
		d.sharedSD.Configure(machine.PinConfig{Mode: machine.PinOutput})
	}
	d.writeU16LittleEndian(uint16(command))
	err := d.writeU16LittleEndian(uint16(command >> 16))
	if len(w) == 0 || err != nil {
		return err
	}
	for _, v := range w {
		d.writeU16LittleEndian(v)
	}
	return nil
}

var shiftLittleEndian32 = [4]int{0, 8, 16, 24}

//go:inline
func (d *Dev) spiWrite32(command uint32, w []uint32) error {
	if sharedDATA {
		d.sharedSD.Configure(machine.PinConfig{Mode: machine.PinOutput})
	}
	err := d.writeU32LittleEndian(command)
	if len(w) == 0 || err != nil {
		return err
	}
	for _, v := range w {
		d.writeU32LittleEndian(v)
	}
	return nil
}

// writeU32LittleEndian writes a 32bit integer over the SPI connection in 32bit, little-endian mode of operation.
//
//go:inline
func (d *Dev) writeU32LittleEndian(v uint32) error {
	d.spi.Transfer(byte(v))
	d.spi.Transfer(byte(v >> 8))
	d.spi.Transfer(byte(v >> 16))
	d.spi.Transfer(byte(v >> 24))
	return nil
}

// writeU32LittleEndian writes a 32bit integer over the SPI connection in 32bit, little-endian mode of operation.
//
//go:inline
func (d *Dev) writeU16LittleEndian(v uint16) error {
	d.spi.Transfer(byte(v))
	d.spi.Transfer(byte(v >> 8))
	return nil
}

//go:inline
func (d *Dev) responseDelay() {
	if responseDelay != 0 {
		// Wait for response.
		waitStart := time.Now()
		for time.Since(waitStart) < responseDelay {
			d.spi.Transfer(0)
		}
	}
}

func (d *Dev) Reset() {
	d.wlRegOn.Low()
	time.Sleep(20 * time.Millisecond)
	d.wlRegOn.High()
	time.Sleep(250 * time.Millisecond)
	// d.irq.Configure(machine.PinConfig{Mode: machine.PinInput})
}

//go:inline
func (d *Dev) gpioSetup() {
	d.wlRegOn.Configure(machine.PinConfig{Mode: machine.PinOutput})
	if sharedDATA {
		d.sharedSD.Configure(machine.PinConfig{Mode: machine.PinOutput})
		d.sharedSD.Low()
	}
	d.cs.Configure(machine.PinConfig{Mode: machine.PinOutput})
	d.cs.High()
}

//go:inline
func make_cmd(write, inc bool, fn Function, addr uint32, sz uint32) uint32 {
	return b2u32(write)<<31 | b2u32(inc)<<30 | uint32(fn)<<28 | (addr&0x1ffff)<<11 | sz
}

//go:inline
func b2u32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}
