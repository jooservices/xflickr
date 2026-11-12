# Setup wizard

The `/onboarding` route is a small host around an ordered registry of setup
pages. Each page declares its success destination, failure destination, and
allowed rollback destinations. The host validates reported transitions and
renders rollback controls from the active page's contract.

The entry resolver chooses the connect result page for a Flickr redirect,
then resumes on the done page when an account exists, the connect page when a
valid key exists, or the key page otherwise. The connect result page consumes
and removes Flickr return parameters before reporting its result.

Keys, accounts, and the selected key are shared through setup data context.
Page components own their form, authorization, or crawl behavior; the shared
shell owns progress, focus, live announcements, and load states.
