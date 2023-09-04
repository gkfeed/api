from fastapi import FastAPI
from .add import add_feed
from .feed import get_rss_feed
from .list import get_subscriptions_list
from .delete import delete_feed


def setup(app: FastAPI):
    app.add_api_route('/feed', get_rss_feed)
    app.add_api_route('/add', add_feed)
    app.add_api_route('/list', get_subscriptions_list)
    app.add_api_route('/delete', delete_feed)
