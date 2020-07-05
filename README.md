CBSD RESTFull API sample in golang


Init:

set GOPATH

go get
go run ./cbsd-mq-api [ -l listen]


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
