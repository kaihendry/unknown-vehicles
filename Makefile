STACK = protect
VERSION = $(shell git describe --tags --always --dirty)
export SAM_CLI_TELEMETRY=0

DOMAINNAME = protect.dabase.com
ACMCERTIFICATEARN = arn:aws:acm:eu-west-2:407461997746:certificate/9083a66b-72b6-448d-9bce-6ee2e2e52e36

deploy:
	sam build
	sam deploy --no-progressbar \
		--s3-bucket hendry-lambdas \
		--s3-prefix protect \
		--stack-name $(STACK) \
		--parameter-overrides \
			DomainName=$(DOMAINNAME) \
			Version=$(VERSION) \
			ACMCertificateArn=$(ACMCERTIFICATEARN) \
			PushoverToken=$(PUSHOVER_TOKEN) \
			PushoverUserKey=$(PUSHOVER_USER_KEY) \
		--no-confirm-changeset \
		--no-fail-on-empty-changeset \
		--capabilities CAPABILITY_IAM \
		--disable-rollback

build-MainFunction:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ${ARTIFACTS_DIR}/bootstrap

validate:
	aws cloudformation validate-template --template-body file://template.yml

destroy:
	aws cloudformation delete-stack --stack-name $(STACK)

test:
	@echo "Sending test webhook..."
	@curl -X POST -H "Content-Type: application/json" \
	-d '{"type": "motion", "camera": "Front Door Cam", "timestamp": "2023-10-27T10:00:00Z", "message": "Motion detected at the front door"}' \
	http://localhost:3000/

tail:
	sam logs --stack-name $(STACK) --tail

logs:
	sam logs --stack-name $(STACK)

list:
	sam list endpoints --stack-name $(STACK)

traces:
	sam traces --stack-name $(STACK)

sync:
	sam sync --stack-name $(STACK)

clean:
	rm -rf main gin-bin
