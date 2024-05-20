CONFIG_PATH=${HOME}/.loghouse

.PHONY: init
init:
	mkdir -p ${CONFIG_PATH}

.PHONY: gencert
gencert:
	cfssl gencert -initca resources/ca-csr.json | cfssljson -bare ca
	cfssl gencert -ca=ca.pem \
			-ca-key=ca-key.pem \
			-config=resources/ca-config.json \
			-profile=server \
			resources/server-csr.json | cfssljson -bare server
	cfssl gencert -ca=ca.pem \
			-ca-key=ca-key.pem \
			-config=resources/ca-config.json \
			-profile=client \
			-cn="root" \
			resources/client-csr.json | cfssljson -bare root-client
	cfssl gencert -ca=ca.pem \
			-ca-key=ca-key.pem \
			-config=resources/ca-config.json \
			-profile=client \
			-cn="nobody" \
			resources/client-csr.json | cfssljson -bare nobody-client
	mv *.pem *.csr ${CONFIG_PATH}
	cp resources/model.conf $(CONFIG_PATH)
	cp resources/policy.csv $(CONFIG_PATH)

.PHONY: compile
compile:
	protoc api/v1/*.proto \
			--go_out=. \
			--go-grpc_out=. \
			--go_opt=paths=source_relative \
			--go-grpc_opt=paths=source_relative \
			--proto_path=.

.PHONY: test
test:
	go clean -testcache
	go test -race ./...