## 1. Prove the defects (red)

- [ ] 1.1 Add a failing table-driven test in `notification_test.go` asserting that a draft with an oversized title, an oversized data payload, too many links, an over-long relation name and an over-long href is each refused by `Draft.Validate`. Follow the `table-test` skill. Verify with `go test -run 'TestDraftValidateContentLimits' -count=1 ./...` and confirm every case fails because the draft was *accepted* — not because a constant does not compile yet (see `.claude/rules/prove-errors-with-tests.md`).
- [ ] 1.2 Add a failing test asserting that a draft carrying a `javascript:` href is refused, and that an `https` href and a relative href are accepted. Verify it fails on the `javascript:` case only, proving the other two already hold.
- [ ] 1.3 Add a failing test in the core asserting that `ListQuery{Kinds: <101 values>}` and `ListQuery{States: <101 values>}` are each refused by `Validate`. Verify with `go test -run 'TestListQueryValidateFilterLimits' -count=1 ./...` and confirm it fails because both validate clean today.
- [ ] 1.4 Add a failing test in `service_publish_test.go` asserting that `Publish` with more than the draft limit refuses before writing anything, checking that the store mock receives no `Insert` call. Verify it fails because the publish succeeded.
- [ ] 1.5 Add a failing test in `http_test.go` issuing `GET /v1/notifications` with a query string carrying more parameters than the standard library will parse, asserting a `400` with a validation code. Verify it fails by returning `200` with an unfiltered listing — record that observed behaviour in the test's comment, since it is the defect being fixed.

## 2. Carry the limits (green)

- [ ] 2.1 Add the named constants from `design.md` D1 (title, data, links, relation name, href, drafts per publish, filter values) to `notification.go` and `store.go`, each with godoc naming what it bounds, and correct the constant block's comment at `notification.go:37-39` so it describes what is actually enforced. Verify with `go build ./...` and by reading the comment back against the DDL.
- [ ] 2.2 Add the `Limits` value carrying those numbers plus the link-scheme policy, with an unset field meaning the default, and a `Limits` accessor or resolver that fills defaults in one place. Verify with a unit test asserting a zero `Limits` resolves to exactly the constants from 2.1.
- [ ] 2.3 Add the service option that supplies `Limits`, and validate it in `New`: a non-positive limit, or naming permitted link schemes while also disabling the check, is a `ConfigurationError` before any traffic. Follow the validation style of `email_dispatcher.go:287-324`. Verify with a table-driven test covering each contradictory and non-positive case (spec scenarios "A meaningless limit is refused at construction" and "Naming schemes and opting out at once is refused at construction").

## 3. Enforce on the publish path

- [ ] 3.1 Add the content checks to draft validation against a supplied `Limits`, keeping `Draft.Validate()` working and validating against the defaults, per `design.md` D3. Verify the tests from 1.1 now pass and no existing test in the root module changed behaviour.
- [ ] 3.2 Add the link-scheme check, refusing a draft whose href uses a scheme the policy does not permit, without rewriting or normalising any accepted href. Verify the test from 1.2 passes and that an accepted href is returned byte for byte (spec scenario "Ordinary links are accepted by default").
- [ ] 3.3 Make `Service.Publish` validate against the service's configured limits and refuse more than the draft cap before opening any transaction. Verify the test from 1.4 passes and that the refusal is a `ValidationError`, not a `ConfigurationError`.
- [ ] 3.4 Apply the same content limits to a close's successor, which shares `validateContent` (`notification.go:159`), and add a test that an oversized successor payload is refused by `CloseRequest.Validate`. Verify with `go test -run 'TestCloseRequestValidate' -count=1 ./...`.

## 4. Enforce on the read path

- [ ] 4.1 Add the filter-count bound to `ListQuery.Validate`, naming the filter in the issue pointer. Verify the test from 1.3 passes and that the cursor's fingerprint behaviour is unchanged (`go test -run 'TestCursor' -count=1 ./...`).
- [ ] 4.2 Make the listing handler parse `r.URL.RawQuery` itself and answer a validation error when parsing fails, per `design.md` D6. Verify the test from 1.5 passes and that a normal listing with filters still works.
- [ ] 4.3 Add a test asserting the other endpoints fail closed on an unparseable query — the stream falls back to the acting user rather than following someone else. Verify with `go test -run 'TestStream' -count=1 ./...`.

## 5. Cover the overrides

- [ ] 5.1 Add tests showing one consumer override per limit class: a raised data limit accepting a payload between the default and the new bound and returning it byte for byte, a raised filter bound, and a raised draft cap. Required by `.claude/rules/library-design.md` ("in tests, the default is covered, and so is at least one consumer override"). Verify all pass.
- [ ] 5.2 Add tests for the scheme overrides: a host naming `mailto` as permitted, and a host disabling the check and publishing an arbitrary scheme (spec scenarios "A host permits another scheme" and "A host opts out of the check"). Verify both pass.

## 6. Document and verify

- [ ] 6.1 Update `docs/notifications.md`: add the content limits, the draft cap, the filter bound and the link-scheme policy to the defaults-and-overrides table, each naming its override, and add the page-size-times-payload product to the stated-limits section per `design.md` Risks. Verify by re-reading: every new limit has a named override beside it.
- [ ] 6.2 Run `make all` and verify lint, split-check and tests pass on every module.
- [ ] 6.3 Re-run the 1.1–1.5 tests with each fix temporarily reverted and verify each fails again, confirming the tests prove the defects rather than passing vacuously.
- [ ] 6.4 Run `openspec validate "bound-publish-and-query-inputs" --strict` and verify every spec scenario has a corresponding test.
