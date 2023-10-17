from fastapi import FastAPI
from .add import add_feed, add_feed_lazy
from .feed import get_rss_feed
from .list import get_subscriptions_list
from .delete import delete_feed


def setup(app: FastAPI):
    app.add_api_route("/api/v1/feed", get_rss_feed)
    app.add_api_route("/api/v1/add", add_feed, methods=["POST"])
    app.add_api_route("/api/v1/list", get_subscriptions_list)
    app.add_api_route("/api/v1/delete", delete_feed)
    app.add_api_route("/api/v1/add_lazy", add_feed_lazy, methods=["POST"])
