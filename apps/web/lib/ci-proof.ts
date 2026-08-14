// TEMPORARY, and this branch is never merged. It exists to prove that the
// ENT-214 format check can actually fail, per the repository's rule that a
// test which cannot fail is worse than no test.
//
// Everything below is deliberately wrong for prettier.config.mjs: double
// quotes where the config says single, trailing semicolons where it says
// none, and spacing no formatter would produce. It still typechecks and still
// passes ESLint, so the only job that may go red is the format check.
export const ciProof = {   name:"ci-proof",    deliberatelyUnformatted: true };

export function describeCiProof( ):string {
        return   `${ciProof.name} exists only to turn CI red`  ;
}
