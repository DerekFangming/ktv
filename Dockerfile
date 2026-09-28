# Build from the project directory:
#   docker build -t videoplayer .
#
# Run with the song folder mounted at /videos. The catalog file .data.db is
# created in that folder. Transcodes are stored in the video-cache volume.
#   docker run --rm -p 8080:8080 \
#     -v /Users/fangming.ning/Documents/video:/videos \
#     -v video-cache:/app/.cache \
#     videoplayer
#
# Open the player with this machine's LAN address, not localhost, so the QR
# code points at a URL the phone can reach.

FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/videoplayer .

FROM alpine:3.20

RUN apk add --no-cache ffmpeg

WORKDIR /app
COPY --from=build /out/videoplayer /app/videoplayer

ENV VIDEO_DIR=/videos
ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/app/videoplayer"]
