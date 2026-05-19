My family have a lot of photos scattered across computers over time - and we have been bad at organising them. 
We struggle with platform dependent gallery software that is too overly featured (like iphoto) - but also individually mapping the photos into drive paths results in duplication, because we don't want to lose data, so just copy the entire subdirectory between computers. 

The task is to build a photo management and gallery software webapp, runnable locally, that will:

a) recursively scan a directory and find all jpg/jpegs in the tree, potentially that match a filename regex e.g. "starts with DSCN_", "does not end with..." - and then check the exif of these to ensure they are taken with a camera that belongs to the family (we will do this to avoid picking up on e.g. other random downloads, social media pictures from outside the family or anything else). We will then hash these to dedupe them - by listing the photo alongside its filepath in a database. This will allow the identification of filepath subtrees that are full of duplicates, but surpassed by a later version (e.g. in an old file tree, you would expect all the photos to be a subset of a more modern one, potentially - because the more modern subtree would have photos from 2026 missing from the 2024 photo archive). This would be a useful feature. 

b) display these photos, potentially allowing scrolling between them in a manner that respects their original file structure OR based on other filter/search results (see below for examples of geographical or timeline/datetime clustering). e.g. photos by image capture date. 

c) have exif display/categorisation features, such as projecting the photos onto a map, and allowing geosearch (eg. "all photos taken in spain - or as a simplified MVP to avoid dependence and checking of country and geographical markers, within X miles of this lat/long point clicked on a map, displayed with a circular radius marker). 

d) Timeline view - allowing zoom in or out of timeline, perhaps with chart in background of count of photos taken on that time period. This allows rapidly navigating to time periods where photos were taken.

d) storing sufficient information in the database that the information can be used for either rule based or ML based clustering (e.g. photos taken in quick succession or on the same concurrent set of days in the same geographical location represneent an event )

This should be aimed towards self-hosting first and foremost, and single user initially. 