# CBSD RESTFull API

Copyright (c) 2013-2025, The CBSD Development Team
Homepage: https://github.com/cbsd/cbsd

#### Table of Contents

1. [Project Description - What does the project do?](#description)
2. [Installation - HowTo start](#installation)
3. [Usage - Configuration options and additional functionality](#usage)
4. [Contributing - Contribute to the project](#contributing)
5. [Support - Mailing List, Talks, Contacts](#support)


## Description

Provides a simplified API for creating and destroying CBSD virtual environments.

## Errata

By default, all actions are permitted for all requests.
Through the `-allowlist <whitelist_file>` parameter you can limit the number of permissible public keys/CID.
Format of <whitelist_file> similar to authotized_keys: one key per line, e.g:

```
ssh-ed25519 AAAA...xxx your_name@@your.domain
ssh-ed25519 AAAA...yyy user2@@example.com
```

## Installation

Assuming you have a stock vanilla FreeBSD 14.2+ installation.
The directives below configure a standalone installation ( single API + hoster env),
however you can use any number of physical nodes for single API.

1) Install packages:
```
pkg install -y cbsd cbsd-mq-router cbsd-mq-api beanstalkd git
```

2) Configure beanstalkd, the broker service.

  Since all services are on the same server, we will specify the address 127.0.0.1
  for incoming connections and start the service:
```
sysrc beanstalkd_flags="-l 127.0.0.1 -p 11300"
service beanstalkd enable
service beanstalkd restart
```

3) Configure CBSD as usual:
```
env workdir=/usr/jails /usr/local/cbsd/sudoexec/initenv
```

4) Configure MQ router

First, get hoster FQDN via `hostname` command.
Let's say your host has a name: apitest.my.domain

Open /usr/local/etc/cbsd-mq-router.json in any favorite editor and set
"tube" and "reply_tube_prefix" params ( cbsd_<hostname_without_dot> and cbsd_<hostname_without_dot>_result_id ), e.g:

```
{
    "cbsdenv": "/usr/jails",
    "cbsdcolor": false,
    "broker": "beanstalkd",
    "logfile": "/dev/stdout",
    "recomendation": "/usr/local/cbsd/modules/api.d/misc/recomendation.sh",
    "freejname": "/usr/local/cbsd/modules/api.d/misc/freejname.sh",
    "server_url": "https://127.0.0.1",
    "cloud_images_list": "/usr/local/etc/cbsd_api_cloud_images.json",
    "iso_images_list": "/usr/local/etc/cbsd_api_iso_images.json",
    "beanstalkd": {
      "uri": "127.0.0.1:11300",
      "tube": "cbsd_zpool1",
      "reply_tube_prefix": "cbsd_zpool1_result_id",
      "reconnect_timeout": 5,
      "reserve_timeout": 5,
      "publish_timeout": 5,
      "logdir": "/var/log/cbsdmq"
    }
}

```

  `cloud_images_list` - The path to the json  file, which is displayed upon /images query - list of avaliable images.
                        See etc/cbsd_api_cloud_images.json as sample.

5) Start MQ router:
```
service cbsd-mq-router enable
service cbsd-mq-router start
```

6) Install CBSD API module:
```
cbsd module mode=install api
echo 'api.d' >> ~cbsd/etc/modules.conf
cbsd initenv
```

7) Configure CBSD API module.

Copy configuration sample to work dir:
```
cp -a /usr/local/cbsd/modules/api.d/etc/api.conf ~cbsd/etc/
cp -a /usr/local/cbsd/modules/api.d/etc/bhyve-api.conf ~cbsd/etc/
cp -a /usr/local/cbsd/modules/api.d/etc/jail-api.conf ~cbsd/etc/
```

Open ~cbsd/etc/api.conf in any favorite editor and set "server_list=" to server FQDN, e.g:
```
...
server_list="apitest.my.domain"
...
```

Set 'cbsd' user permission for ~cbsd/etc/api.conf file:
```
chown cbsd:cbsd ~cbsd/etc/api.conf
```

Here you can check that the API module scripts works:
```
su -m cbsd -c '/usr/local/cbsd/modules/api.d/misc/recomendation.sh'
```
must return the host from server_list ( apitest.my.domain )

```
su -m cbsd -c '/usr/local/cbsd/modules/api.d/misc/freejname.sh'
```
must return the unique name 'envX'.

8) Configure RestAPI daemon:
```
mkdir -p /var/db/cbsd-api /usr/jails/var/db/api/map
chown -R cbsd:cbsd /var/db/cbsd-api /usr/jails/var/db/api/map
service cbsd-mq-api enable
service cbsd-mq-api start
```

## Usage

### Via curl, valid endpoints:

```
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/cluster
curl -X POST -H "Content-Type: application/json" -d @filename.json http://127.0.0.1:65531/api/v1/create/vm1
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/status/<env>
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/start/<env>
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/stop/<env>
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/destroy/<env>
```
Where `<cid>` is your token/namespace. For convenience, in a *private cluster*, 
we suggest using md5 hash of your public key as <cid>.

To test with curl, create valid payload file, e.g. `debian12.json`:
```
cat > debian11.json <<EOF
{
  "imgsize": "10g",
  "ram": "1g",
  "cpus": 2,
  "image": "debian12",
  "pubkey": "ssh-ed25519 AAAA..XXX your@localhost"
}
EOF
```
Then send it to /create endpoint:
```
curl --no-progress-meter -X POST -H "Content-Type: application/json" -d @debian12.json http://127.0.0.1:65531/api/v1/create/vm1
```

to create 'vm1' or:
```
curl --no-progress-meter -X POST -H "Content-Type: application/json" -d @debian12.json http://127.0.0.1:65531/api/v1/create/_
```

to assign a VM name automatically.

### Via CBSDfile:

To test via CBSDfile, lets create simple CBSDfile, where CLOUD_KEY - is your publickey string:
```
CLOUD_URL="http://127.0.0.1:65531"
CLOUD_KEY="ssh-ed25519 AAAA..XXX your@localhost"

jail_minio1()
{
	imgsize="10g"
	pkg_bootstrap=0
}
```

Run:
```
sudo cbsd up
```

After jail start you can use:
```
cbsd login
cbsd status
cbsd destroy
```

See documentation for detailed information and additional examples [here](https://github.com/cbsd/cbsd/blob/develop/share/docs/general/cbsd_api.md)

## Get Support

* GitHub: https://github.com/cbsd/cbsd-mq-api/issues
* For CBSD-related support, discussion and talks, please join to Telegram CBSD usergroup channel: @cbsdofficial ( [https://t.me/cbsdofficial](https://t.me/cbsdofficial)

## update go sub/mods

```
rm -f go.mod go.sum
```

```
go mod init cbsd-mq-api
go mod tidy
```

## Support Us

* https://www.patreon.com/clonos
