UNAME_S := $(shell uname -s)

all: 
	@./build.sh

clean:
	rm -f cbsd-mq-api
	rm -rf src

install: all
	install cbsd-mq-api /usr/local/sbin
ifeq ($(UNAME_S),Linux)
	install systemd/cbsd-mq-api.service /lib/systemd/system/cbsd-mq-api.service
	systemctl daemon-reload
	@test -d /var/log/cbsdmq || mkdir -m 0755 /var/log/cbsdmq
	@test -d /var/log/cbsd_mq_api || mkdir -m 0755 /var/log/cbsd_mq_api
	@chown cbsd:cbsd /var/log/cbsdmq /var/log/cbsd_mq_api
	@test -r /etc/cbsd-mq-api.json || sed 's:/dev/stdout:/var/log/cbsd_mq_api/cbsd_mq_api.log:g' etc/cbsd-mq-api.json > /etc/cbsd-mq-api.json
else
	install rc.d/cbsd-mq-api /usr/local/etc/rc.d/cbsd-mq-api
endif

uninstall:
ifeq ($(UNAME_S),Linux)
	rm -f /usr/local/sbin/cbsd-mq-api /lib/systemd/system/cbsd-mq-api.service
else
	rm -f /usr/local/sbin/cbsd-mq-api /usr/local/etc/rc.d/cbsd-mq-api
endif
