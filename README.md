# CBSD RESTFull API

Copyright (c) 2013-2021, The CBSD Development Team

Homepage: https://bsdstore.ru

## Description

Provides a simplified API for creating and destroying CBSD virtual environments.

#### Table of Contents

1. [Project Description - What does the project do?](#project-description)
2. [Usage - Configuration options and additional functionality](#usage)
3. [Contributing - Contribute to the project](#contributing)
4. [Support - Mailing List, Talks, Contacts](#support)

## Usage


Init:

set GOPATH

go get
go run ./cbsd-mq-api [ -l listen]


# Install

mkdir -p /var/db/cbsd-api /usr/jails/var/db/api/map
chown -R cbsd:cbsd /var/db/cbsd-api /usr/jails/var/db/api/map

Install api.d module + enable in modules.conf, cbsd initenv.

Setup api.d module: make sure

    "recomendation": "/usr/local/cbsd/modules/api.d/misc/recomendation.sh",
    "freejname": "/usr/local/cbsd/modules/api.d/misc/freejname.sh",

script works (from cbsd user): chown cbsd:cbsd ~cbsd/etc/api.conf

# On host

1) pkg install -y sysutils/cbsd-mq-router

2) setup cbsd-mq-router.json, e.g:

```
{
    "cbsdenv": "/usr/jails",
    "cbsdcolor": false,
    "broker": "beanstalkd",
    "logfile": "/dev/stdout",
    "beanstalkd": {
      "uri": "127.0.0.1:11300",
      "tube": "cbsd_host1_example_com",
      "reply_tube_prefix": "cbsd_host1_example_com_result_id",
      "reconnect_timeout": 5,
      "reserve_timeout": 5,
      "publish_timeout": 5,
      "logdir": "/var/log/cloudmq"
    }
}
```

3) service cbsd-mq-router enable
4) service cbsd-mq-router start

## Usage

Valid endpoints:

```
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/cluster
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/status/<env>
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/start/<env>
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/stop/<env>
curl -H "cid:<cid>" http://127.0.0.1:65531/api/v1/destroy/<env>
```

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
