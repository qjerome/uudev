# uudev
uudev (User Udev) allows to run unprivileged hooks on udev events

# Build a hook

The first thing to do is to monitor some udev events and output the
results in some file (it will be useful later).

```bash
uudev -m | tee /tmp/uudev.json
```

Then you have to wait, or trigger the event you want to build a hook
for. For example, if you want to execute a hook when a USB device is
plugged, unplug it and plug it again.

Once you get the events you want, you can stop with `Ctrl + c` the 
previous `uudev -m` command.

Now you have to find exactly **the event** you want to hook. It is 
highly recommended to do this task using [jq](https://jqlang.github.io/jq/).

Once you have found the event you are interested in, you have to create 
a **uudev** hook for it. 

```bash
# will print a template rule from specific events selected using jq
# the second jq command is used to select useful fields
jq 'select (.FIELD_A == "xyz")' /tmp/uudev.json | jq '{FIELD_X, FIELD_Y}'  | uudev -t
```

If you are happy with it you can dump it in a config file and edit it.

Please see [examples directory](./examples) for concrete examples.
