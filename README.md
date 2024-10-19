# What is MQTT Relay Module Timer?

This is a simple program that responds to MQTT requests by adding more seconds to a countdown timer.
In its concept, it's just replacing a mechanical countdown timer (found in old and cheap ovens, for example).

Alone though, it's not very useful, unless you setup an MQTT app to send requests to this program.

# Why did I wrote this?

Together with a [time table automation](https://github.com/supercomputer7/timetable-automation), I can use this project as replacement for manual & mechanical timing solutions for automating various tasks.

# Simple build instructions

You would want to build this for a Raspberry Pi or some platform that support GPIO devices.
If you just want to test this, run:
```sh
go build .
```

## Statically cross-compiling for ARM64

For a Raspberry Pi or other ARM64 platform you can run this line so you have a statically-compiled binary:
```sh
CGO_ENABLED=0 GOARCH=arm64 go build -ldflags="-extldflags=-static" .
```

# Limitations

There are some limitations which are probably OK for most people:
- No backup MQTT broker, which means that if you don't have a connection to a MQTT broker anymore, you simply have a "dead" switch.
- Although there's a limit on the max amount of seconds to wait before setting the latch off, it's technically possible to spam the MQTT channel indefinitely, hanging the timer ON forever.
- When running from the terminal, you need to supply credentials to the MQTT broker in order to connect. Otherwise empty ones will be sent.
- If you stop the program before it sets the latch off, the latch will stay on until next run!

# License, dependencies & derived works

As usual with my projects, this project is licensed under the MIT license which I think fits
this project in the best way being possible.

Be sure to check the dependencies' licenses if you want to use them in your project, as not all of
them are under the MIT license.

Suggestions or derived works from this are more than welcome!
If this helped you in any way, I'd be very happy to hear about this :)
