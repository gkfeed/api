from locust import HttpUser, between, task


class WebsiteUser(HttpUser):
    wait_time = between(5, 15)

    @task
    def list(self):
        self.client.get("/list")

    @task
    def feed(self):
        self.client.get("/feed")
