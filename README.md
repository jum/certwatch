# certwatch

Tool to watch a caddy certmagic redis store, implemented via https://github.com/pberkel/caddy-storage-redis

to install:

```
$ go build
$ install -c certwatch /usr/local/bin
```

Use a certwatch.service like this, replace your domains, services and redis url accordingly:

```
[Unit]
Description=Watch for cert changes and restart services

[Service]
ExecStart=/usr/local/bin/certwatch -debug -redisurl redis://redis.tailXXXXX.ts.net -cmd="systemctl restart postfix dovecot coturn" mail.example.org imap.example.org turn.example.org

[Install]
WantedBy=multi-user.target
```
