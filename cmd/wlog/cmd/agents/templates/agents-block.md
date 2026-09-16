## Logging with wlog

This project logs with wlog, one wide event per unit of work.

- Add context with `wlog.Set` and `wlog.SetGroup`; report failures with `wlog.Error`.
- Name fields in `snake_case`. Never write a password, token, or card number.
- Record an audit fact for a login, role change, refund, export, or deletion.
- Run `wlog map --min-score 80 ./...` before a pull request touches a handler.

Skills for the details live in `.agents/skills`.
