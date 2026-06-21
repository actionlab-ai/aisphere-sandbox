ARG BASE_IMAGE=debian:12-slim
FROM ${BASE_IMAGE}
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates python3 bash curl git ripgrep fd-find jq tree chromium && rm -rf /var/lib/apt/lists/* && useradd -u 1000 -m sandbox
COPY bin/aisphere-tool-server.py /usr/local/bin/aisphere-tool-server.py
COPY tools/default-tools.json /opt/aisphere/tools/default-tools.json
COPY skills /opt/aisphere/skills
RUN chmod +x /usr/local/bin/aisphere-tool-server.py && mkdir -p /workspace && chown -R 1000:1000 /workspace /opt/aisphere
USER 1000:1000
ENV AISPHERE_WORKSPACE=/workspace AISPHERE_TOOL_PORT=18081
EXPOSE 18081 9222 6080
ENTRYPOINT ["/usr/local/bin/aisphere-tool-server.py"]
