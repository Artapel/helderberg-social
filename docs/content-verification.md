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
5. If the organiser says it has stopped, do not delete it: give it a `status`
   (see the README) with their announcement as `link`, empty the schedule days,
   and keep watching their news for the restart.

## Tabletop, card games and miniatures pass, 5 September 2026

Asked for: activities like Imperium Games, The Jokers House, WildStorm Studios /
Untamed Creations and the Durban Golden Brush, in the Helderberg. What the
search (web, unplugyourself.co.za's organiser list, and Facebook groups and
pages as the page profile) found:

- **Imperium Games, Somerset West** (Helderberg Village Walk): the one real
  tabletop shop in the area, with weekly open play. Listed under the new
  `games` category with the shop's own schedule; its Facebook events stop in
  March 2024, so the nights are on the shop's word only. Its events page is a
  watched source.
- **WildStorm Studios, Somerset West**: miniature artist and community host
  (Discord), Untamed Creations resin range, judge/entrant at Golden Brush
  (Durban Wargaming Club) and Go Figure (Comic Con Africa). Listed as a group.
- **The Warren, Somerset West** (Meerlust Industrial Park): tabletop shop on
  unplugyourself.co.za with events to September 2024; its Facebook page is
  gone and thewarren.co.za is a 2 KB placeholder. Not listed; probably closed.
- **The Jokers House** is a trading-card shop with Friday play nights but is
  not in the Helderberg (no address on the site; not found in the area).
- **No Helderberg board-game club, D&D group, wargaming club or painting
  meet-up exists on Facebook or the web.** The players use Cape Town-wide
  groups, listed as joining candidates in `docs/facebook.md`.
- **Golden Brush** is in Durban and Comic Con Africa is in Johannesburg and
  Cape Town; neither is a Helderberg event.

Found this way on 5 September 2026: **Somerset West parkrun has been closed
indefinitely since 29 May 2025** (construction across the course; no
replacement venue agreed with the City). The site had it as a live Saturday
run. It is now marked paused, and the API watches the run's news feed for the
restart post. parkrun's own event map lists no parkrun anywhere in the
Helderberg; the nearest are Mitchells Plain, Bellville and Betty's Bay.

## Research pass, 4 September 2026

44 listings were added from a desk search of Helderberg wine estates, markets,
sports and social clubs, community groups and venues. Every one is `verified: false` and
**none has coordinates**: the map skips them until a real position is confirmed on the
organiser's own page or on a visit (geocoders have placed Helderberg addresses in the wrong
town before, so do not paste a geocoder result). Each needs the four steps above plus
`coords`.

| id | name | type | website | note |
|---|---|---|---|---|
| `avontuur-estate` | Avontuur Estate | place | <https://www.tripadvisor.com/Attraction_Review-g469396-d5888097-Reviews-Avontuur_Wine_Estate-Somerset_West_Western_Cape.html> |  |
| `idiom-restaurant-wine-tasting-centre` | Idiom Restaurant & Wine Tasting Centre | place | <https://idiom.co.za> |  |
| `journeys-end-vineyards` | Journey's End Vineyards | place | <https://journeysend.co.za> |  |
| `longridge-wine-estate` | Longridge Wine Estate | place | <https://longridge.co.za> |  |
| `wedderwill-wine-estate` | Wedderwill Wine Estate | place | none recorded | Limited independent verification of current opening status; treat opening hours as unconfirmed |
| `waterkloof-wines` | Waterkloof Wines | place | <https://www.waterkloofwines.co.za> |  |
| `country-craft-market-southeys-vines` | Country Craft Market at Southey's Vines | activity | <https://countrycraftmarket.org> |  |
| `gordonsbaai-kersmark` | Gordonsbaai Kersmark (Gordon's Bay Christmas Market) | activity | <https://capemarkets.co.za/christmas-markets/gordonsbaai-kersmark/> |  |
| `wine-lands-cycling-club` | Wine Lands Cycling Club | group | <https://www.winelandscyclingclub.co.za> | Appears to be the renamed/expanded successor to Wannabees Cycling Club already listed on the site; kept separate since trail network, permit system and membership numbers differ materially from what's on file |
| `southey-vines-geelsloot-bike-park` | Southey Vines / Geelsloot Bike Park | place | none recorded |  |
| `u3a-helderberg` | U3A Helderberg | group | <https://sites.google.com/site/u3ahelderberg/home> |  |
| `u3a-helderberg-hikers` | U3A Helderberg Hikers | group | <https://sites.google.com/site/u3ahelderberghikers> |  |
| `vergelegen-trail-guided-hike` | The Vergelegen Trail (guided hike) | activity | <https://vergelegen.co.za/vergelegen-trail/> | Confirmed 2025 season dates only (20 Sep, 18 Oct, 1/15/29 Nov); 2026 season dates not yet published as of this research |
| `rotary-club-somerset-west` | Rotary Club of Somerset West | group | <https://www.facebook.com/RotaryClubofSomersetWest/> |  |
| `somerset-west-ratepayers-association` | Somerset West Ratepayers Association (SWRA) | group | <https://swratepayers.co.za> |  |
| `somerset-west-community-association` | Somerset West Community Association (SWCA) | group | none recorded | Newly founded (2025); limited independent info beyond this news article |
| `helderberg-chess-club` | Helderberg Chess Club | group | <https://chesshub.org.za/directory/helderberg-chess-club/> |  |
| `grand-slam-bridge-club` | Grand Slam Bridge Club | group | none recorded | Sources disagree on exact meeting day (Tuesday vs Wednesday) - unconfirmed |
| `helderberg-village-bridge-canasta-club` | Bridge & Canasta Club at The Lord Somerset Clubhouse | group | none recorded |  |
| `somerset-west-cricket-club` | Somerset West Cricket Club (SWCC) | group | <https://swcc.co.za> |  |
| `helderberg-rugby-club` | Helderberg Rugby Football Club | group | <https://helderbergrugbyklub.co.za> |  |
| `somerset-west-golf-club` | Somerset West Golf Club | place | none recorded |  |
| `swcc-tennis-club` | Somerset West Country Club Tennis (SWCC Tennis) | group | <https://www.facebook.com/SWCCTennis> |  |
| `somerset-west-bowls-club-country-club` | Somerset West Bowls Club | group | <https://www.facebook.com/groups/somersetwestbowls/> |  |
| `helderberg-bowling-club` | Helderberg Bowling Club | group | <https://www.facebook.com/helderbergbowls> |  |
| `fairview-golf-estate` | Fairview Golf Estate | place | <https://fairview-golfestate.co.za> |  |
| `helderberg-village-golf-course` | Helderberg Village Golf Course | place | none recorded |  |
| `helderberg-photographic-society` | Helderberg Photographic Society | group | <https://helderbergphoto.com> |  |
| `helderberg-village-choir` | Helderberg Village Choir | group | none recorded |  |
| `helderberg-village-arts-crafts-society` | Helderberg Village Arts & Crafts Society | group | none recorded |  |
| `playhouse-theatre-somerset-west` | The Playhouse Theatre, Somerset West | place | none recorded |  |
| `monkey-town-venue` | Monkey Town Venue | place | <https://www.monkeys.co.za> | No longer has animal/primate viewing as of April 2024; now a swimming/picnic day venue only |
| `strand-athletics-club` | Strand Athletics Club (SAC) | group | <https://strandac.co.za> |  |
| `helderberg-trail-running-instagram` | Helderberg Trail Running (@runhelderberg) | online | <https://www.instagram.com/runhelderberg/> |  |
| `gordons-bay-boat-angling-club` | Gordon's Bay Boat Angling Club | group | <https://gbbac.co.za> |  |
| `sailing-academy-gb` | Sailing Academy GB | group | <https://sailingacademy.co.za/venues/gordons-bay-yacht-club> |  |
| `dive-and-adventure-gordons-bay` | Dive and Adventure Gordon's Bay | place | none recorded |  |
| `asla-indoor-sports-club` | ASLA Indoor Sports Club | place | none recorded | Only found referenced in a general area guide; could not independently confirm schedule or exact address |
| `waterstone-village` | Waterstone Village | place | <https://www.waterstonevillage.co.za> |  |
| `somerset-west-community-facebook-group` | Somerset West Community (Facebook group) | online | <https://www.facebook.com/groups/somersetwest1/> |  |
| `somerset-west-information-facebook-group` | Somerset West Information (Facebook group) | online | <https://www.facebook.com/groups/swest.info/> |  |
| `somerset-west-strand-live-work-facebook-group` | I live / work in Somerset West / Strand (Facebook group) | online | <https://www.facebook.com/groups/183819464996787/> |  |
| `helderberg-chess-academy` | Helderberg Chess Academy | group | <https://www.facebook.com/helderbergchessacademy/> | Could not confirm exact relationship to Helderberg Chess Club (same organisers, separate brand, or distinct entity) |
| `mtb-lourensford` | MTB@Lourensford | activity | none recorded |  |
