FROM golang:1.9-alpine
LABEL maintainer="Timothy Bogdala <tdb@animal-machine.com>"

VOLUME /data

RUN apk add --no-cache --virtual .build-deps git g++ \
    && go get -u github.com/golang/dep/cmd/dep \
    && go get github.com/tbogdala/filefreezer \
    && cd /go/src/github.com/tbogdala/filefreezer \
    && dep ensure \
    && cd cmd/freezer \
    && go install \
    && cd /go \
    && rm -r /go/src/github.com \
    && rm /go/bin/dep \
    && apk del .build-deps \
    && rm -rf /var/cache/apk/*

EXPOSE 8080

WORKDIR /data

CMD ["--db=file:/data/freezer.db", "serve", ":8080"]
ENTRYPOINT ["/go/bin/freezer"]

# before running the filefreezer server, users need to be created in the database.
# this can be done by overriding the default parameters to run the `user add` command:
#
# sudo docker run --rm -v /home/timothy/testdata:/data \
#                 -p 127.0.0.1:8040:8080 filefreezer:latest user add -u admin -p 1234


# after the users have been added, the server can be run in a container with a 
# command like the following which will be accessed from localhost:8040 and have 
# the data persisted in the testdata directory:
# 
# sudo docker run --rm -v /home/timothy/testdata:/data \
#                 -p 127.0.0.1:8040:8080 filefreezer:latest

