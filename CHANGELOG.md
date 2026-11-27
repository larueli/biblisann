# Changelog

## [0.1.1](https://github.com/larueli/biblisann/compare/v0.1.0...v0.1.1) (2026-11-27)


### Bug Fixes

* **release:** use version-only tags ([#8](https://github.com/larueli/biblisann/issues/8)) ([f9b1d0f](https://github.com/larueli/biblisann/commit/f9b1d0f8c452ef7f7aa43dba8853da67db9dff88))
* **scim:** bound pagination offsets ([#10](https://github.com/larueli/biblisann/issues/10)) ([cb0c01c](https://github.com/larueli/biblisann/commit/cb0c01c9fb15e851c1e31d0bc20ea2861d09b067))

## 0.1.0 (2026-11-26)


### ⚠ BREAKING CHANGES

* BIBLISANN_COVERS_DIR is replaced by BIBLISANN_DATA_DIR, with local covers stored in its covers subdirectory.

### Features

* admin screens, browser Web Push, shelf view, OAuth token families ([88db855](https://github.com/larueli/biblisann/commit/88db855b224ccccea8e0a0de59f096a036b70d7d))
* **api:** per-user request quota with RateLimit headers, paced bulk ISBN list, covers of imports spooled to disk ([38a1656](https://github.com/larueli/biblisann/commit/38a1656bb6c0285e0f9de47a3b291d8fbc5d856b))
* **auth:** OIDC sign-in with group sync, SCIM 2.0 provisioning ([8ccd3ee](https://github.com/larueli/biblisann/commit/8ccd3ee6d9568ce8231bdf554aa1df725f96e23e))
* **auth:** single-use step-up for providers without auth_time, admin-set passwords, no self-service password for SSO accounts ([2a1fb75](https://github.com/larueli/biblisann/commit/2a1fb75aa8b050b7eb97f25d57509717641b06a4))
* **auth:** step-up authentication before sensitive actions ([ca46982](https://github.com/larueli/biblisann/commit/ca469822cf4608c4dca30b231043ef6b768cf67f))
* **auth:** TOTP two-factor, recovery codes and password reset by email ([0c10917](https://github.com/larueli/biblisann/commit/0c109179bdf8de0a96cdb895668168dbe3bc5b17))
* **catalog:** administrators edit, refresh, exclude and delete shared editions ([9423b51](https://github.com/larueli/biblisann/commit/9423b5143051602fbcd160c93609a84b014a3c09))
* **catalog:** editors and library administrators can start metadata refreshes for their books and libraries ([1af9f9f](https://github.com/larueli/biblisann/commit/1af9f9fc307fca700e91f10c909cb7a09e45ecaa))
* **catalog:** expose the cover file check result as Prometheus gauges ([df55905](https://github.com/larueli/biblisann/commit/df55905687fe764c4c928787878ec7d324fda75e))
* **catalog:** refresh works for books no provider knows and records the result ([cbf32d8](https://github.com/larueli/biblisann/commit/cbf32d84eec204c0da99eca47e2130548b87fb16))
* **catalog:** weekly and on-demand metadata refresh of books providers did not know ([a8c5aff](https://github.com/larueli/biblisann/commit/a8c5aff2c93448a8625f8dce828513f64294ce99))
* **catalog:** weekly job and command that report covers whose files are missing from storage ([a02087f](https://github.com/larueli/biblisann/commit/a02087ffd9b57b0959d67e85a8da81b66605ad6a))
* extra metadata providers, addons, Valkey rate limiting, OPDS, web auth screens ([21f4967](https://github.com/larueli/biblisann/commit/21f4967da3095e016f0fbcaf5e14f622417c576d))
* first commit ([14b37d5](https://github.com/larueli/biblisann/commit/14b37d5a5b6bf9f45ea65b707ebe4d27fd459160))
* garbage collection, observability, self-hosted API docs, Hardcover two-way sync ([d2b9fd6](https://github.com/larueli/biblisann/commit/d2b9fd65543965d250355df93a8823f8ecceaede))
* **helm:** mount the metrics token as a file and validate the chart with kubeconform and kube-linter ([e68d861](https://github.com/larueli/biblisann/commit/e68d8616e5818302a7e3d31e5f4d132239749be2))
* **loans:** every member with loan.write sees and manages the loans of a shared library ([18075fe](https://github.com/larueli/biblisann/commit/18075fe6dd1f2679543439eafb7c47833a1390c7))
* **notify:** confirm email channels with a mailed code and cap mails per user per day ([a357cff](https://github.com/larueli/biblisann/commit/a357cffe7cb5404d203e5de12eae9e09c8941522))
* **oauth:** OAuth 2.1 authorization server for MCP (PKCE, registration, refresh rotation) ([2ad5eba](https://github.com/larueli/biblisann/commit/2ad5eba473ffd7051553742ecf7e3e17298ebd25))
* **sharing:** invitations for unknown addresses, same answer for non-administrators ([df16e2d](https://github.com/larueli/biblisann/commit/df16e2d71381a3b60704416b97de29db78c28d1c))
* slice 0 foundations ([33458d2](https://github.com/larueli/biblisann/commit/33458d2cdacd79422f576b0d1c15856251b5b077))
* slice 1 scan and own ([7030b3a](https://github.com/larueli/biblisann/commit/7030b3aea9871e3974f2dcf634b406927ba09452))
* slice 2 backend (sharing, reading, lists, loans, notifications, MCP, backup, import) ([58a5a7c](https://github.com/larueli/biblisann/commit/58a5a7cb2c87119d7f079c450a6929ec75a553d0))
* slice 3 (OIDC, passkeys, SCIM, OAuth server, providers, OPDS, Valkey) and docs ([0620498](https://github.com/larueli/biblisann/commit/06204986bdffd8fd2dd30f8642f5267d1a41475f))
* StoryGraph and LibraryThing import, bulk Hardcover ratings, data export/import, account deletion, revocation ([7e4ef57](https://github.com/larueli/biblisann/commit/7e4ef57a72c75cc261266f85e9442ff9780b4260))
* **web:** slice 2 UI, docs and ADRs ([595a87f](https://github.com/larueli/biblisann/commit/595a87f11cf8b53d4035ea5d53f0c22255fbbb16))


### Bug Fixes

* **api:** meter MCP calls and OPDS feeds in the request quota, weigh MCP add_book like a scan ([83b5e32](https://github.com/larueli/biblisann/commit/83b5e324bc50de44aaaa115fd7ff5219b2635b02))
* **catalog,admin:** service-layer administrator check for catalog changes, step-up before deleting another user ([982c91e](https://github.com/larueli/biblisann/commit/982c91e4297068b59ecdece67f1d04ad84920140))
* **docs:** silence the Biome important-style warning on the Redoc page ([dae018d](https://github.com/larueli/biblisann/commit/dae018d5109915921392d0165f0e110adbd5b75e))
* harden security and rename data directory ([948fe48](https://github.com/larueli/biblisann/commit/948fe485ab30e172108ebe5bdce08a6e2cd4edad))
* **loans:** the calendar feed only lists libraries where the owner still manages loans ([7ef7b9b](https://github.com/larueli/biblisann/commit/7ef7b9b0be668e02a55b155fce78667d68c0f65a))
* security audit findings, secret-key startup check, compose example, floating image tags ([f447a38](https://github.com/larueli/biblisann/commit/f447a38cb7425086974e0a49f398f895653b26cf))
* **security:** apply the review of the follow-ups ([3555758](https://github.com/larueli/biblisann/commit/3555758f0b587a65ef4c3116977453eee6173939))
* **web:** serve the index.html of a directory such as /docs/ ([ded4151](https://github.com/larueli/biblisann/commit/ded4151161a9ce2ba87355642c67a431bf7f9b1d))


### Documentation

* add security policy and contributing guide ([091b681](https://github.com/larueli/biblisann/commit/091b6818e3762b95508581ffac39e94bf85e0a78))
* API guide, Helm settings, ADR 0026 additions for the follow-up features ([1698e72](https://github.com/larueli/biblisann/commit/1698e72d3730fbd5dc60f4c1f63c22c9488ba53c))
* document repository creation, the releaser App and moving the repository ([a9d6de4](https://github.com/larueli/biblisann/commit/a9d6de4bbbdd040e492246a01b30c612f9da33d6))
* fix stale roadmap entries for quotas, backup step-up and make task ([712cf10](https://github.com/larueli/biblisann/commit/712cf100272732999f794026cdc95704189a6e50))
* handoff after the security follow-ups ([3874f52](https://github.com/larueli/biblisann/commit/3874f5283bdd3226bebd46ab7f274abd7ce2ddc0))
* honesty rules and ADR 0026 for the security follow-ups ([33efe9e](https://github.com/larueli/biblisann/commit/33efe9e9f2075a47703cd3fc283eefbcbcd986b9))
* say that the release workflow needs the releaser App ([56974a3](https://github.com/larueli/biblisann/commit/56974a35bb95f4f4855b4d6cbdc7be34b3a256ee))
* update the GitHub setup, roadmap and handoff after the first CI run ([e59c7ff](https://github.com/larueli/biblisann/commit/e59c7ffbbbd4a3e704226dda25fe022f88ba2134))
