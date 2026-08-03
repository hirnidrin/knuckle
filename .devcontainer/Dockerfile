FROM golang:1.26.5-bookworm

# ISO build dependencies (mirrors .github/workflows/ci.yml + release.yml).
# squashfs-tools and gnupg aren't installed by CI because the ubuntu-latest
# runner image ships them already — a devcontainer base image doesn't.
RUN apt-get update && apt-get install -y --no-install-recommends \
    xorriso \
    mtools \
    cpio \
    systemd-boot-efi \
    squashfs-tools \
    gnupg \
    curl \
    ca-certificates \
    sudo \
    xz-utils \
    && rm -rf /var/lib/apt/lists/*

# just — task runner used for all repo workflows (just ci, just build, just iso)
RUN curl -sSfL https://just.systems/install.sh | bash -s -- --to /usr/local/bin

# Non-root user matching devcontainer conventions (the golang base image runs as root)
ARG USERNAME=vscode
RUN useradd --create-home --shell /bin/bash $USERNAME \
    && echo "$USERNAME ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME

ENV GOPATH=/home/$USERNAME/go
ENV PATH=$GOPATH/bin:$PATH
# Debian's /etc/profile resets PATH for login shells, dropping /usr/local/go/bin
# and $GOPATH/bin — re-add them so `bash -l` (and postCreateCommand) still see go/just.
RUN echo "export GOPATH=$GOPATH" > /etc/profile.d/go-path.sh \
    && echo 'export PATH=/usr/local/go/bin:$GOPATH/bin:$PATH' >> /etc/profile.d/go-path.sh

USER $USERNAME
