// Package dockerfile provides a parser and builder for converting Dockerfiles
// into cc.InstanceSource objects. It enables building VM images directly from
// Dockerfiles without external Docker tooling.
//
// The package supports a subset of Dockerfile syntax:
//   - FROM image[:tag] [AS name]
//   - RUN command (shell form) or RUN ["executable", "arg1", ...] (exec form)
//   - COPY [--chown=user:group] src... dst
//   - ADD src... dst (local files only, no URL or archive extraction)
//   - ENV key=value ...
//   - WORKDIR /path
//   - ARG name[=default]
//   - USER user[:group]
//   - EXPOSE port[/protocol] ...
//   - LABEL key=value ...
//   - CMD command or CMD ["executable", "arg1", ...]
//   - ENTRYPOINT command or ENTRYPOINT ["executable", "arg1", ...]
//   - SHELL ["executable", "arg1", ...]
//   - STOPSIGNAL signal
//   - Heredoc syntax in RUN commands (e.g., RUN cat > /file <<'EOF')
//
// Unsupported features (v1):
//   - Multi-stage builds (COPY --from=stage)
//   - ADD with URLs or archive extraction
//   - Docker BuildKit heredoc for COPY/ADD (COPY <<EOF /path)
//   - HEALTHCHECK
//   - ONBUILD
//   - VOLUME
//
// Example usage:
//
//	dockerfile := []byte(`
//	    FROM alpine:3.19
//	    RUN apk add --no-cache curl
//	    COPY app /usr/local/bin/
//	    CMD ["app"]
//	`)
//	source, err := cc.BuildDockerfileSource(ctx, dockerfile,
//	    cc.WithBuildContext(dirContext),
//	    cc.WithDockerfileCacheDir(cacheDir),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	inst, err := cc.New(source)
package dockerfile
