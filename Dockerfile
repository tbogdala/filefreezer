# FROM golang:latest-alpine
FROM alpine:latest
LABEL maintainer="Timothy Bogdala <tdb@animal-machine.com>"

VOLUME /data

RUN apk add --no-cache --virtual .build-deps go git g++ \
    && apk add --no-cache ca-certificates \
    && go get -u github.com/golang/dep/cmd/dep \
    && go get github.com/tbogdala/filefreezer \
    && cd /root/go/src/github.com/tbogdala/filefreezer \
    && /root/go/bin/dep ensure \
    && cd cmd/freezer \
    && go install \
    && rm -r /root/go/src \
    && rm /root/go/bin/dep \
    && apk del .build-deps \
    && apk add --no-cache ca-certificates \
    && rm -rf /var/cache/apk/*

EXPOSE 8080

WORKDIR /data

CMD ["--db=file:/data/freezer.db", "serve", ":8080"]
ENTRYPOINT ["/root/go/bin/freezer"]

# the server can be run in a container with a  command like the following which will 
# be accessed from localhost:8040 and have the data persisted in the testdata directory
# of the user running the command (and the "$HOME/testdata" can be replaced with whatever
# the user needs):
# 
# sudo docker run --read-only --rm -v $HOME/testdata:/data -p 127.0.0.1:8040:8080 filefreezer:latest

# in order to use the service, users need to be created in the database. the command to do this
# can be run before or after the server is started.
#
# to add a user, override the default parameters to run the `user add` command:
#
# sudo docker run --rm -v $HOME/testdata:/data filefreezer:latest user add -u admin -p 1234


# after the server is running and the user has been addedthe normal command line client 
# can be used to query the server in the container:
#
# freezer -u admin -p 1234 -s secret -h localhost:8040 file ls