FROM alpine:latest
LABEL maintainer="Timothy Bogdala <tdb@animal-machine.com>"

VOLUME /data

RUN apk add --no-cache ca-certificates 

RUN apk add --no-cache --virtual .build-deps go git g++ \
    && go get -u github.com/golang/dep/cmd/dep \
    && go get github.com/tbogdala/filefreezer \
    && cd /root/go/src/github.com/tbogdala/filefreezer \
    && /root/go/bin/dep ensure \
    && cd cmd/freezer \
    && go install \
    && rm -r /root/go/src \
    && rm /root/go/bin/dep \
    && apk del .build-deps \
    && rm -rf /var/cache/apk/*

EXPOSE 8080

WORKDIR /data

CMD ["--db=file:/data/freezer.db", "serve", ":8080"]
ENTRYPOINT ["/root/go/bin/freezer"]

# NOTES
# ======

# typical update process:

# remove any existing images of tbogdala/filefreezer if necessary:
# sudo docker rmi <imagenum> --force

# build a new image:
# sudo docker build -t tbogdala/filefreezer .

# tag the image (e.g. v0.9.0):
# sudo docker tag <imagenum> tbogdala/filefreezer:0.9.0

# update docker hub
# sudo docker push tbogdala/filefreezer