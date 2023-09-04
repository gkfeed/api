from tortoise import fields
from tortoise.models import Model


class Item(Model):
    feed_id = fields.IntField()
    title = fields.TextField()
    text = fields.TextField()
    date = fields.DatetimeField()
    link = fields.TextField()
