/**
 * Whether approving a finding creates a record, and therefore whether there is
 * anything for the Hands to prepare (ENT-278).
 *
 * # A COPY OF WHAT `domain/records/registers.go` KNOWS, AND DELIBERATELY THIN
 *
 * core-api holds the registers, their columns and their labels, and refuses an
 * explanation for a finding whose approval creates nothing. This list decides
 * one thing only: whether to offer the control at all. A control that always
 * refused for a whole class of findings would teach a person that the feature
 * is broken, which is the ENT-202 lesson about controls that visibly do
 * nothing.
 *
 * It is not a second authority and must not grow into one. The names of the
 * columns, what may be filled and from which facts, and the register's label
 * all come back from core-api on the response, so nothing here has an opinion
 * about a customer's record. If this list falls behind the Go one, the cost is
 * a control that is missing from a finding it would have worked on, not a
 * control that promises something the server will not do.
 *
 * `review` is absent, and it is the ordinary case: it is every finding whose
 * obligation has no action type classified, approving one records the decision
 * and creates nothing, and asking an agent to explain a record that will not
 * exist is asking it to write fiction.
 */
const CREATES_A_RECORD = new Set([
  'create_ropa',
  'create_dsar',
  'create_ai_system',
])

export function createsARecord(actionType?: string): boolean {
  return actionType ? CREATES_A_RECORD.has(actionType) : false
}

/**
 * Whether a finding is still open to a decision.
 *
 * The Hands prepares a proposal for a decision somebody is about to make. Once
 * a finding is approved, core-api refuses to prepare anything for it at all,
 * because an approval enqueues the execution and the payload stops being a
 * proposal the instant something is going to act on it. Offering the control
 * there would be offering a button whose only outcome is a refusal.
 *
 * A rejected finding is the same case for a plainer reason: nothing is going to
 * be created, so there is nothing to explain.
 */
export function awaitingADecision(status?: string): boolean {
  return status !== 'approved' && status !== 'rejected'
}
