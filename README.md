# CBSD RESTFull API

Copyright (c) 2013-2021, The CBSD Development Team

Homepage: https://bsdstore.ru

#### Table of Contents

1. [Project Description - What does the project do?](#description)
2. [Installation - HowTo start](#installation)
3. [Usage - Configuration options and additional functionality](#usage)
4. [Contributing - Contribute to the project](#contributing)
5. [Support - Mailing List, Talks, Contacts](#support)


## Description

Provides a simplified API for creating and destroying CBSD virtual environments.

## Installation

Assuming you have a stock vanilla FreeBSD 13.0+ installation.
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
    "beanstalkd": {
      "uri": "127.0.0.1:11300",
      "tube": "cbsd_apitest_my_domain",
      "reply_tube_prefix": "cbsd_cbsd_apitest_my_domain_result_id",
      "reconnect_timeout": 5,
      "reserve_timeout": 5,
      "publish_timeout": 5,
      "logdir": "/var/log/cbsdmq"
    }
}
```

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

Set 'cbsd' user permission to ~cbsd/etc/api.conf:
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

Valid endpoints:

```
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/cluster
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/status/<env>
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/start/<env>
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/stop/<env>
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/destroy/<env>
```

To test, lets create simple CBSDfile, where CLOUD_KEY - is your publickey string:
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

See documentation for detailed information and additional examples: [https://www.bsdstore.ru/en/cbsd_api_ssi.html](https://www.bsdstore.ru/en/cbsd_api_ssi.html)

## Contributing

* Fork me on GitHub: [https://github.com/cbsd/cbsd-mq-api.git](https://github.com/cbsd/cbsd-mq-api.git)
* Switch to 'develop' branch
* Commit your changes (`git commit -am 'Added some feature'`)
* Push to the branch (`git push`)
* Create new Pull Request

## Support

* For CBSD-related support, discussion and talks, please join to Telegram CBSD usergroup channel: @cbsdofficial
* Web link: https://t.me/cbsdofficial
* Or subscribe to mailing list by sending email to: cbsd+subscribe@lists.tilda.center
* Other contact: https://www.bsdstore.ru/en/feedback.html
