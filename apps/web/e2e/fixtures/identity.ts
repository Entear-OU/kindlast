import { execFile, spawn } from 'node:child_process'
import { createHash, randomBytes } from 'node:crypto'
import path from 'node:path'
import { promisify } from 'node:util'

const run = promisify(execFile)

/**
 * Creating a person in the identity provider, without sending any mail.
 *
 * The signup journey normally ends at a verification code, which is exactly
 * the half a browser test cannot drive. Zitadel's import endpoint takes the
 * verified flag and the password in the same call, so a fixture user arrives
 * already able to sign in. That is not a shortcut around the product's real
 * behaviour: verification is Zitadel's concern and Zitadel's test surface, and
 * what this repository owns is everything after the redirect comes back.
 *
 * Mail does now work on this stack, so this is a choice about speed and blast
 * radius rather than a workaround. A journey test that waited for a message
 * would depend on Mailpit, on Zitadel's notifier, and on delivery timing, none
 * of which is the code under test, and would fail for reasons that teach
 * nothing about this repository.
 *
 * The reason mail used to go nowhere is worth keeping, because the error names
 * the wrong thing: Zitadel refuses an SMTP provider that has no credentials
 * and reports it as `SMTPConfig.NotFound`, while its own API and projection
 * both show the config present and active (zitadel/zitadel#8344). The seed job
 * now sets credentials Mailpit does not need for exactly this reason.
 */

// Which Zitadel, for a machine that may be running several (ENT-250). A
// worktree's stack publishes auth on its own port, and this fixture creating a
// user in a sibling branch's identity provider would leave that branch's suite
// with a person it never made. `scripts/stack-env.sh` exports both names; the
// second is the one compose itself reads, so setting only that still lands
// here. In a single checkout it is 8300, as documented everywhere else.
const ZITADEL_URL =
  process.env.KINDLAST_AUTH_URL ??
  `http://localhost:${process.env.KINDLAST_AUTH_PORT ?? '8300'}`
// Absolute, because docker resolves -f against the working directory and this
// file sits four levels below the repository root.
const COMPOSE_FILE = path.resolve(__dirname, '../../../../deploy/compose.yaml')

/**
 * A development-only credential for throwaway users this fixture creates and
 * deletes. It exists in the repository for the same reason the compose
 * passwords do: it is a local value with no reach beyond a disposable stack.
 */
export const FIXTURE_PASSWORD = 'Passw0rd!-e2e-fixture'

export interface FixtureUser {
  id: string
  username: string
  email: string
}

/**
 * The seed bot's personal access token, which Zitadel writes into a named
 * volume at first-instance setup. There is no host-side path to it, so the
 * only way in is a container that mounts the same volume.
 */
let cachedToken: string | undefined

async function seedBotToken(): Promise<string> {
  if (cachedToken) return cachedToken

  const { stdout } = await run('docker', [
    'compose',
    '-f',
    COMPOSE_FILE,
    'run',
    '--rm',
    '--no-deps',
    '-T',
    '--entrypoint',
    'sh',
    'seed',
    '-c',
    'cat /machinekey/seed-bot-pat.txt',
  ])

  const token = stdout.trim()
  if (!token) {
    throw new Error(
      'the seed bot PAT was empty; is the compose stack up? ' +
        'docker compose -f deploy/compose.yaml up -d',
    )
  }

  cachedToken = token
  return token
}

async function management(method: string, path: string, body?: unknown) {
  const response = await fetch(`${ZITADEL_URL}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${await seedBotToken()}`,
      'Content-Type': 'application/json',
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (!response.ok) {
    throw new Error(
      `${method} ${path} -> ${response.status}: ${await response.text()}`,
    )
  }

  return response.json()
}

/**
 * A human who can sign in immediately: email already verified, password
 * already set, and no change demanded on first use. All three matter, because
 * any one of them left at its default parks the browser on a Zitadel screen
 * this test has no business driving.
 */
export async function createVerifiedUser(label: string): Promise<FixtureUser> {
  const unique = `${label}-${Date.now()}-${Math.floor(Math.random() * 1e6)}`
  const email = `${unique}@kindlast.test`

  const created = await management(
    'POST',
    '/management/v1/users/human/_import',
    {
      userName: email,
      profile: { firstName: 'Fixture', lastName: label },
      email: { email, isEmailVerified: true },
      password: FIXTURE_PASSWORD,
      passwordChangeRequired: false,
    },
  )

  return { id: created.userId, username: email, email }
}

/**
 * Count organisations, out of band.
 *
 * Deliberately as the superuser rather than through the application's role.
 * Every table forces row level security, so `kindlast_app` and even the
 * schema owner see nothing without the two session GUCs set, and an assertion
 * that silently counted zero would report idempotence it never checked. A
 * superuser bypasses RLS, which is precisely wrong for the application and
 * exactly right for a test that wants the unfiltered truth.
 */
async function psql(
  sql: string,
  vars: Record<string, string> = {},
): Promise<string> {
  // psql's `--set` plus `:'name'` interpolates as a quoted literal, which is
  // the same reason application code uses bind parameters: a fixture that
  // pasted a value into the statement would be one apostrophe away from
  // rewriting the query.
  const settings = Object.entries(vars).flatMap(([name, value]) => [
    '--set',
    `${name}=${value}`,
  ])

  // THE STATEMENT GOES IN ON STDIN, AND IT HAS TO (ENT-264).
  //
  // This said `-tAc <sql>` when it was written, and with a `--set` variable in
  // the statement that is a syntax error every time: psql does not interpolate
  // into the string given to `-c`, it hands it to the server as it stands, and
  // the server has no idea what `:'email'` is. `-f -` is the same statement
  // read as a script, which is the path where variables are substituted.
  //
  // It went in unnoticed because nothing ran this suite. That is the whole of
  // what ENT-264 is about, and this is the second thing wiring the gate found.
  const child = spawn('docker', [
    'compose',
    '-f',
    COMPOSE_FILE,
    'exec',
    '-T',
    '-e',
    `PGPASSWORD=${process.env.KINDLAST_PG_SUPER_PASSWORD ?? 'postgres-dev-password'}`,
    'postgres-app',
    'psql',
    '-U',
    'postgres',
    '-d',
    'kindlast',
    '-v',
    'ON_ERROR_STOP=1',
    ...settings,
    '-tA',
    '-f',
    '-',
  ])

  return new Promise<string>((resolve, reject) => {
    let stdout = ''
    let stderr = ''
    child.stdout.on('data', (chunk) => (stdout += chunk))
    child.stderr.on('data', (chunk) => (stderr += chunk))
    child.on('error', reject)
    child.on('close', (code) => {
      // ON_ERROR_STOP makes psql exit non-zero on a failed statement, so a
      // fixture that wrote nothing fails here rather than returning an empty
      // string that a caller then reads as "no rows".
      if (code === 0) resolve(stdout.trim())
      else reject(new Error(`psql exited ${code}: ${stderr.trim()}`))
    })
    child.stdin.end(sql)
  })
}

export async function countOrganisations(): Promise<number> {
  const count = Number(await psql('select count(*) from organisations'))
  if (!Number.isInteger(count)) {
    throw new Error('could not read an organisation count')
  }
  return count
}

/**
 * The same hash core-api stores, and it has to stay the same.
 *
 * `postgres.HashInvitationToken` is `hex(sha256(token))`. A fixture that
 * drifted from it would mint invitations nothing could ever redeem, and the
 * test asserting a refusal would pass for the wrong reason.
 */
function hashInvitationToken(token: string): string {
  return createHash('sha256').update(token).digest('hex')
}

/**
 * An invitation into an organisation, addressed to whoever you say.
 *
 * Written straight to the table rather than driven through the console,
 * because the console's invite form ends at an email and the token only exists
 * in that message. Scraping Mailpit would make the test depend on Zitadel's
 * notifier and on delivery timing, neither of which is the code under test,
 * and the row is the whole of what redemption reads.
 *
 * As the superuser, like `countOrganisations` and for the same reason: every
 * table forces row level security, so a fixture holding no session GUCs writes
 * nothing at all through the application role.
 */
export async function mintInvitation(
  orgSlug: string,
  email: string,
): Promise<string> {
  const token = randomBytes(32).toString('base64url')

  const id = await psql(
    `insert into invitations (org_id, email, role, token_hash, expires_at)
     select id, :'email', 'member', :'hash', now() + interval '7 days'
     from organisations where slug = :'slug'
     returning id`,
    { email, hash: hashInvitationToken(token), slug: orgSlug },
  )

  if (!id) {
    throw new Error(`no organisation with the slug ${orgSlug} to invite into`)
  }
  return token
}

/**
 * Whether an invitation is still there to be used.
 *
 * Three answers rather than a boolean, so that a token which never reached the
 * table cannot be reported as one that survived a refusal. That is the failure
 * an assertion about "unused" is most likely to hide.
 */
export async function invitationState(
  token: string,
): Promise<'missing' | 'used' | 'unused'> {
  const state = await psql(
    `select case
       when count(*) = 0 then 'missing'
       when count(*) filter (where accepted_at is not null) > 0 then 'used'
       else 'unused'
     end
     from invitations where token_hash = :'hash'`,
    { hash: hashInvitationToken(token) },
  )

  if (state !== 'missing' && state !== 'used' && state !== 'unused') {
    throw new Error(`could not read the invitation's state, got: ${state}`)
  }
  return state
}

export async function deleteUser(id: string): Promise<void> {
  try {
    await management('DELETE', `/management/v1/users/${id}`)
  } catch {
    // A fixture that fails to clean up must not fail the run it belongs to.
    // The users are disposable and the stack is torn down with `down -v`.
  }
}
