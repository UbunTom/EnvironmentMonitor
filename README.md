# EnvironmentMonitor

A program that reads environmental values (temperature, pressure and humidity) from a BME280 sensor and writes them to an InfluxDB database.

Values can be averaged to reduce measurement noise.


## Building

Install `golang`, then build the executable:

```bash
go build
```

## Usage

Create an InfluxDB database called `environment`, then run the command:

```bash
./environmentmonitor <averaging window size> <polling interval>
```
