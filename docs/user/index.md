# User guide

Biblisann keeps track of the physical books you own: where they are, whether you read them, and whether they are lent.

!!! note "Status"
    Biblisann is `0.x`: features land slice by slice (see the [roadmap](../roadmap.md)).

- [First run](first-run.md): create the administrator, a library, and add your first books.

- [Install](install.md): Docker Compose, container, binary, Kubernetes.
- [Configuration](configuration.md): every setting and how to provide secrets.

## Languages

The interface follows your browser language (English and French) and falls back to English. You can change it from the language selector; the choice is remembered on that device.

## API

The OpenAPI 3.1 description of the REST API is served by your own instance, without authentication, at `/openapi.json` and `/openapi.yaml`.
