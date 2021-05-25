FROM golang

WORKDIR /opt

ADD . /opt

ENV GOOS js

ENV GOARCH wasm

RUN go get

RUN go build -o ./frontend.wasm

FROM nginx

COPY --from=0 /opt/fonts /usr/share/nginx/html/fonts
COPY --from=0 /opt/frontend.wasm /usr/share/nginx/html/frontend.wasm
COPY --from=0 /opt/index.html /usr/share/nginx/html/index.html
COPY --from=0 /opt/main.css /usr/share/nginx/html/main.css
COPY --from=0 /opt/wasm_exec.js /usr/share/nginx/html/wasm_exec.js

#fix wasm mime issue
COPY --from=0 /opt/mime.types /etc/nginx/mime.types


