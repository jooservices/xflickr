# Setup wizard

The `/onboarding` route is a small host around an ordered registry of setup
pages. Each page declares its success destination, failure destination, and
allowed rollback destinations. The host validates reported transitions and
renders rollback controls from the active page's contract. Reports are bound to
their issuing page visit; a report from a page that is no longer current is
ignored, including a report from an earlier visit to the same page id.

The entry resolver chooses the connect result page for a Flickr redirect,
then resumes on the done page when an account exists, the connect page when a
valid key exists, or the key page otherwise. The connect result page consumes
and removes Flickr return parameters before reporting its result. For
compatibility, a valid key with no account and `?step=3` resumes on the empty
done page.

Keys, accounts, the selected key, and an unfinished key form draft are shared
through setup data context. Page components own their form, authorization, or
crawl behavior; the shared shell owns progress, focus, live announcements, and
load states. A terminal success is declared by the done page's `next: null`
contract, and the host navigates home with the completion toast.
