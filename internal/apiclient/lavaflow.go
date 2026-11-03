package apiclient

// LavaflowHumanMergeMode is LavaFlow's default merge mode, in which a person
// approves each merge. Its wire value isn't settled (human or
// person_approves): the API's contract decides, and this is the one place
// that names it.
const LavaflowHumanMergeMode = Human
