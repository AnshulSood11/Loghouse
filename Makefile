CONFIG_PATH=${HOME}/.loghouse
RESOURCES_PATH=`pwd`/resources

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
	mv *.pem *.csr ${RESOURCES_PATH}

.PHONY: cp_resources
cp_resources:
	mkdir -p ${CONFIG_PATH}
	cp ${RESOURCES_PATH}/* ${CONFIG_PATH}

.PHONY: compile
compile:
	protoc api/v1/*.proto \
			--go_out=. \
			--go-grpc_out=. \
			--go_opt=paths=source_relative \
			--go-grpc_opt=paths=source_relative \
			--proto_path=.

.PHONY: build
build: compile gencert

.PHONY: test
test: cp_resources
	go clean -testcache
	CONFIG_PATH=`pwd`/resources go test -race ./...

TAG ?= 0.0.1
build-image:
	docker build -t github.com/anshulsood11/loghouse:$(TAG) .

.PHONY: deploy
deploy:
	 helm upgrade loghouse deploy/loghouse --namespace=loghouse --create-namespace --install