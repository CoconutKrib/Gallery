TODO

* Allow manually adding a lat long to a photo, if the GPS tags are missing - store this in the database as a manual addition sidecar style piece of data. Also allow an approximation flag on this, where we're uncertain (with potentially a radius). 

* Notes/descriptions fields? 

* Export of data/metadata?

* Update spec to always use  RFC3339 representations when interacting with the sqlite database, as this is the format that sqlite's strftime understands natively.

* filename filter - include then exclude (e.g exclude beats include), and check for case insensitivity (jpg, JPG etc, DSCN dscn...)