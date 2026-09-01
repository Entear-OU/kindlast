/**
 * What one exchange with Kindy produced, as the composer draws it.
 *
 * Plain types in their own module so the client composer and the server
 * action can share them without either importing the other's runtime: the
 * composer must stay renderable in a test, and the action pulls in
 * `next/headers` the moment it is imported.
 *
 * The states are not collapsed, the ask panel's own rule: a refusal is a
 * guardrail working, a failure is something broken, no-model is a fact about
 * the deployment, nothing-open is a fact about the organisation, and choose
 * is Kindy asking a question back. Drawing any of them as "sorry" would
 * misreport the product's most important behaviour.
 */

/**
 * The finding an exchange is about, named so a reader can check it (ENT-284).
 *
 * Always both halves. An id on its own is a subject only the server can read,
 * and the panel's job here is to let somebody notice that Kindy is answering
 * about the wrong finding before they act on the answer.
 */
export interface KindySubject {
  findingId: string
  findingTitle: string
}

export type KindyState =
  | { status: 'idle' }
  | ({
      status: 'answered'
      question: string
      answer: string
    } & KindySubject)
  | ({
      status: 'refused'
      question: string
      reason: string
    } & KindySubject)
  /**
   * The question was asked away from any finding, so Kindy asks which one it
   * is about rather than choosing (ENT-284). The question rides along so that
   * choosing does not mean typing it again.
   */
  | {
      status: 'choose'
      question: string
      choices: KindySubject[]
    }
  | {
      status: 'nothing-open'
      question: string
    }
  | { status: 'unavailable'; question: string }
  | { status: 'error'; question: string; message: string }

export const KINDY_IDLE: KindyState = { status: 'idle' }

export type KindyAction = (
  previous: KindyState,
  form: FormData,
) => Promise<KindyState>
