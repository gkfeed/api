from tortoise import fields
from tortoise.models import Model


class Feed(Model):
    title = fields.TextField()
    url = fields.TextField()
    type = fields.TextField()
