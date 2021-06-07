package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/devices/v3/bmxx80"
	"periph.io/x/host/v3"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

func getBus() i2c.BusCloser {
	// Open a handle to the first available I²C bus:
	bus, err := i2creg.Open("")
	if err != nil {
		log.Fatal(err)
	}

	return bus
}

func getDevice(bus i2c.BusCloser) *bmxx80.Dev {
	// Open a handle to a bme280/bmp280 connected on the I²C bus using default
	// settings:
	dev, err := bmxx80.NewI2C(bus, 0x76, &bmxx80.DefaultOpts)
	if err != nil {
		log.Fatal(err)
	}

	return dev
}

func computeSum(steps int, input <-chan physic.Env, output chan<- physic.Env) {
	// Read up to `steps` values from `input`, totalling the sum of each input
	// Once `steps` inputs have been received, the sum is written to the `output` channel

	defer fmt.Println("computeSum finished")

	for {
		total := physic.Env{}
		for i := 0; i < steps; i++ {
			env, open := <-input
			if !open {
				close(output)
				return
			}

			total.Temperature += env.Temperature
			total.Pressure += env.Pressure
			total.Humidity += env.Humidity

			fmt.Println(env)
		}
		output <- total
	}
}

func averageStream(steps int, logging <-chan physic.Env, averages chan<- physic.Env) {
	// Continuously reads from the `logging` chan, passing the values to the `computeSum`
	// goroutine. When that goroutine outputs a `total`, the values are normalized and
	// sent to the `averages` chan.
	// This function effectively averages values from the `logging` chan with a window of size `steps`

	totals := make(chan physic.Env)
	go computeSum(steps, logging, totals)
	for total := range totals {
		average := physic.Env{}
		divisor := int64(steps)
		average.Temperature = physic.Temperature(int64(total.Temperature) / divisor)
		average.Pressure = physic.Pressure(int64(total.Pressure) / divisor)
		average.Humidity = physic.RelativeHumidity(int64(total.Humidity) / divisor)

		averages <- average
	}
}

func logToDatabase(datapoints <-chan physic.Env) {

	client := influxdb2.NewClient("http://localhost:8086", "")

	writeAPI := client.WriteAPIBlocking("", "environment")

	for data := range datapoints {
		fmt.Print("Writing record")
		fmt.Println(data)

		temp := data.Temperature.Celsius()
		pressure := 0.01 * float64(data.Pressure) / float64(physic.Pascal)
		humidity := float64(data.Humidity) / float64(physic.PercentRH)

		// Create point using full params constructor
		p := influxdb2.NewPoint("env",
			map[string]string{},
			map[string]interface{}{"temp": temp, "pressure": pressure, "humidity": humidity},
			time.Now())
		// write point immediately
		writeAPI.WritePoint(context.Background(), p)

	}
}

func readSensor(dev *bmxx80.Dev, logging chan<- physic.Env) {
	// Read temperature from the sensor:
	var env physic.Env
	if err := dev.Sense(&env); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%8s %10s %9s\n", env.Temperature, env.Pressure, env.Humidity)

	logging <- env
}

func pollInterval(callable func(), interval time.Duration) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	fmt.Println("Hello")

	for {
		select {
		case <-sigs:
			fmt.Println("Signal received")
			return
		case t := <-ticker.C:
			fmt.Println("Tick at", t)
			callable()
		}
	}

}

func parseFlags() (window_size int, read_interval_secs int) {
	flag.IntVar(&window_size, "window", 8, "Size of the averaging window")
	flag.IntVar(&read_interval_secs, "read_interval", 15, "Time to wait between each read of the sensor (s)")
	flag.Parse()

	return
}

func main() {

	window_size, read_interval_secs := parseFlags()

	// Load all the drivers:
	if _, err := host.Init(); err != nil {
		log.Fatal(err)
	}

	// Set up bus and device
	bus := getBus()
	defer bus.Close()

	dev := getDevice(bus)
	defer dev.Halt()

	logging := make(chan physic.Env, 1)
	defer close(logging)
	averaged := make(chan physic.Env, 1)
	defer close(averaged)

	go averageStream(window_size, logging, averaged)

	// Log values from the channel to the database
	go logToDatabase(averaged)

	// Start reading the sensor
	curried := func() {
		readSensor(dev, logging)
	}
	pollInterval(curried, time.Duration(read_interval_secs)*time.Second)
}
