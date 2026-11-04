# Public Spring contract example

A synthetic Java 21 / Spring Boot 3.5.16 MVC application for the [Spring pack](../../packs/spring-boot/README.md). It has no production connection, database, credentials or company-derived code.

- `/greeting` returns a fixed JSON object.
- `/extra` exists only with the `extra` profile.
- SQL is a contract fixture; no migration engine executes it.
- `example.token` is a synthetic placeholder used to prove config values are not exported.

The Maven tests export explicit MockMvc probes and registered mappings for `contract` and `extra`. They require a fresh `naru.output` directory, supplied by the pack adapter. Use the adapter commands in the pack documentation instead of a bare `mvn test`. Generated `target` files stay untracked. The sample is a contract demonstration, not a security-hardened deployment template.
