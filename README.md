CBSD RESTFull API sample in golang


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
--
{
    "cbsdenv": "/usr/jails",
    "cbsdcolor": false,
    "broker": "beanstalkd",
    "logfile": "/dev/stdout",
    "beanstalkd": {
      "uri": "127.0.0.1:11300",
      "tube": "cbsd_mother_olevole_ru",
      "reply_tube_prefix": "cbsd_mother_olevole_ru_result_id",
      "reconnect_timeout": 5,
      "reserve_timeout": 5,
      "publish_timeout": 5,
      "logdir": "/var/log/cloudmq"
    }
}
--

3) service cbsd-mq-router enable
4) service cbsd-mq-router start


Endpoints:

*list bhyve domain*:

curl [-s] [-i] http://127.0.0.1:8081/api/v1/list


*start (f11a) bhyve domain*:

curl -i -X POST http://127.0.0.1:8081/api/v1/start/f111a


*stop (f11a) bhyve domain*:

curl -i -X POST http://127.0.0.1:8081/api/v1/stop/f111a


*remove (f11a) bhyve domain*:

curl -i -X POST http://127.0.0.1:8081/api/v1/remove/f111a


*create new (f11a) bhyve domain (see *.json files for sample)*:

curl -X POST -H "Content-Type: application/json" -d @bhyve_create_minimal.json http://127.0.0.1:8081/api/v1/create/f111a


*list available image*

pre-compiled json, e.g: `cbsd get_bhyve_profiles src=template`, see 'imagelist' config params

curl [-s] [-i] http://127.0.0.1:8081/api/v1/imagelist


This is a just simple example. Contributing is welcome!
