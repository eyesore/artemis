# This information is outdated (since always) - leaving in case some ideas need to be reviewed/used
# Updated README coming soon.

App is responsible for handling http request (authentication, any other operation desired)
App is responsible for creating any hubs or families that the client should belong to, and adding the client to those.

 - rtmes.NewHub
 - rtmes.NewFamily

rtmes will manage the persistence of hubs and families.  It will expose an API to access hubs and families if necessary.

 - rtmes.GetHub(id) *Hub
 - rtmes.Hubs []*Hub
 - Hub.GetFamily(id) *Family
 - Hub.Families []*Family

App then hands the request to `rtmes` to upgrade the connection to WS.  From there, rtmes manages the connection and any interconnectivity to other clients.  It does this through the

- rtmes.NewClient(r, w, families, hub)

A client is created when the connection is opened.  It remains open until the client (device) closes the connection.

The client is responsible for adding itself to 0 or more hubs and 0 or more families.
If the client does not specify a hub, then it will be added to the default hub.
If the client does not specify a family, then it will not be a member of any families.

Families and Clients both implement an interface for responding to events. (EventResponder)

Families and Clients both implement an interface for responding to messages. (MessageResponder)

Families and Clients both implement an interface for responding pushing messages from the server. (MessagePusher)

Events are specific to a Hub - only EventResponders within the Hub that fires the event will respond.
Are there some default events?  Maybe, probably.

Requirements for messages:

Messages must be parsable into an object with an type/name



