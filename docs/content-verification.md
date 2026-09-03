# Content verification

Every listing in `data/data.js` was seeded on 2026-09-02 from public web sources found by search.
**None has been confirmed with the organiser**, so all carry `verified: false` and the site shows an
"Unverified" badge on each. No phone numbers or emails were recorded.

| Listing | Seed source | What still needs confirming |
|---|---|---|
| Helderberg Harriers | helderbergharriers.co.za | Time-trial venue and time, fees |
| Somerset West parkrun | parkrun.co.za/somersetwest | Start location (course page) |
| Wannabees Cycling Club | bicyclesouth.co.za listing, wannabees.co.za | Ride schedule, fees |
| Helderberg Trails | Facebook page | Address, hours, pricing |
| Somerset West Run Crew | villagecollective.co.za sports edit | Meeting days/times, social handle |
| Run/Walk For Life Somerset West | villagecollective.co.za sports edit | Venue, session times |
| Lourensford Market / Twilight Market | lourensford.co.za event pages | Hours each season |
| Helderberg Nature Reserve | Wikipedia | Gate hours, entry fee |
| Helderberg Farm | villagecollective.co.za sports edit | Hours, permit cost |
| Radloff Park | harcourts.co.za area profile | Nothing specific; municipal park |
| Vergelegen Wine Estate | none recorded | Hours, entry fee |
| Somerset West Village Collective | villagecollective.co.za | Nature of the organisation |
| Strand Beach & Promenade | none recorded | Lifeguard season |
| Bikini Beach | privateproperty.co.za area guide | Blue Flag status (remove "Blue Flag-style" if not) |
| Gordon's Bay Yacht Club | privateproperty.co.za area guide | Website, sailing days |
| Gordon's Bay Lions Club | e-clubhouse.org/sites/gordonsbay | Meeting schedule |
| Gordon's Bay Residents Association | Facebook page | Website |
| This is Gordon's Bay and Strand | Facebook group | Still active |
| Gordon's Bay Community (Instagram) | Instagram | Still active |
| Hiking groups on Meetup | meetup.com topic page | Which groups are live |
| Hottentots Holland Nature Reserve | none recorded | Permit process, CapeNature link |
| Sir Lowry's Pass viewpoint | none recorded | Nothing specific |
| Kogel Bay | none recorded | Resort entry fee |
| Scuba diving from Gordon's Bay | privateproperty.co.za area guide | Name specific operators or remove |
| DistrictMail "What's happening" | districtmailhelderberg.co.za | Column still running |

Events: all five are from the Lourensford Market event page (2026/27 season list). Confirm dates on
the page before launch, especially the 4 September opening night.

## How to verify

1. Open the source URL and the organiser's own site.
2. Confirm name, days/times, cost, and that it is still running.
3. Fill `website`, correct `schedule`, fix `coords` (drop `coordsApprox`).
4. Set `verified: true` and note the date in a comment beside it.
